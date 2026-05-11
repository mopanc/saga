package saga

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupLintLayer creates a single in-memory layer rooted at t.TempDir() with a
// minimal meta.yml. Returns the Layer struct and the topics directory path so
// callers can write fixture .md files in.
func setupLintLayer(t *testing.T, scope string) (Layer, string) {
	t.Helper()
	root := t.TempDir()
	notesDir := filepath.Join(root, "topics")
	if err := os.MkdirAll(notesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := fmt.Sprintf("scope: %s\ndisplay_name: lint-test\nnotes_dir: topics/\n", scope)
	if err := os.WriteFile(filepath.Join(root, "meta.yml"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	layer := Layer{
		Scope:    scope,
		RootPath: root,
		NotesDir: notesDir,
		Meta:     Meta{Scope: scope, DisplayName: "lint-test", NotesDir: "topics/"},
	}
	return layer, notesDir
}

// writeFixture writes a topic at notesDir/<slug>.md with whatever frontmatter
// + body the caller provides. The caller is responsible for the slug ↔ title
// alignment (or deliberate mismatch).
func writeFixture(t *testing.T, notesDir, slug, frontmatter, body string) string {
	t.Helper()
	content := "---\n" + frontmatter + "---\n\n" + body + "\n"
	path := filepath.Join(notesDir, slug+".md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// findDiagnostic asserts the report contains a diagnostic for the given
// (filename, category) pair and returns it. Test fails if absent.
func findDiagnostic(t *testing.T, rep *LintReport, filename, category string) Diagnostic {
	t.Helper()
	for _, d := range rep.Diagnostics {
		if filepath.Base(d.FilePath) == filename && d.Category == category {
			return d
		}
	}
	t.Fatalf("no %s diagnostic for %s; got %d diagnostics: %+v", category, filename, len(rep.Diagnostics), rep.Diagnostics)
	return Diagnostic{}
}

// hasDiagnostic is like findDiagnostic but returns bool (for negative
// assertions: "must NOT contain X").
func hasDiagnostic(rep *LintReport, filename, category string) bool {
	for _, d := range rep.Diagnostics {
		if filepath.Base(d.FilePath) == filename && d.Category == category {
			return true
		}
	}
	return false
}

func TestLintCleanTopicProducesNoDiagnostics(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "clean", `id: 01ABC
scope: personal
type: topic
title: Clean
synonyms: []
sensitivity: internal
confidence: tentative
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if rep.HasFindings() {
		t.Errorf("clean topic produced %d diagnostic(s): %+v", len(rep.Diagnostics), rep.Diagnostics)
	}
	if rep.FilesWalked != 1 {
		t.Errorf("FilesWalked = %d, want 1", rep.FilesWalked)
	}
}

func TestLintMissingRequiredFieldIsParseError(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	// Missing `title`.
	writeFixture(t, notes, "missing-title", `id: 01ABC
scope: personal
type: topic
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if rep.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, want 1", rep.ParseErrors)
	}
	d := findDiagnostic(t, rep, "missing-title.md", CategoryMissingField)
	if d.Severity != SeverityError {
		t.Errorf("severity = %q, want error", d.Severity)
	}
}

func TestLintInvalidTypeIsParseError(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "bad-type", `id: 01ABC
scope: personal
type: not-a-real-type
title: Bad type
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if rep.ParseErrors != 1 {
		t.Errorf("ParseErrors = %d, want 1", rep.ParseErrors)
	}
	findDiagnostic(t, rep, "bad-type.md", CategoryInvalidType)
}

func TestLintInvalidEnumOnConfidence(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "bad-confidence", `id: 01ABC
scope: personal
type: topic
title: Bad confidence
confidence: maybe
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "bad-confidence.md", CategoryInvalidEnum)
	if d.Field != "confidence" {
		t.Errorf("Field = %q, want confidence", d.Field)
	}
}

func TestLintInvalidEnumOnLifecycle(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "bad-lifecycle", `id: 01ABC
scope: personal
type: topic
title: Bad lifecycle
confidence: tentative
lifecycle: eternal
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "bad-lifecycle.md", CategoryInvalidEnum)
	if d.Field != "lifecycle" {
		t.Errorf("Field = %q, want lifecycle", d.Field)
	}
}

func TestLintScopeMismatch(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "wrong-scope", `id: 01ABC
scope: project
type: topic
title: Wrong scope
confidence: tentative
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "wrong-scope.md", CategoryScopeMismatch)
	if d.Severity != SeverityError {
		t.Errorf("scope-mismatch must be error, got %q", d.Severity)
	}
}

func TestLintSlugMismatchIsWarn(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	// File slug is "old-slug" but title slugifies to "new-title".
	writeFixture(t, notes, "old-slug", `id: 01ABC
scope: personal
type: topic
title: New Title
confidence: tentative
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "old-slug.md", CategorySlugMismatch)
	if d.Severity != SeverityWarn {
		t.Errorf("slug-mismatch must be warn, got %q", d.Severity)
	}
}

func TestLintSlugInSynonymsSuppressesWarning(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "old-slug", `id: 01ABC
scope: personal
type: topic
title: New Title
synonyms: ["old-slug"]
confidence: tentative
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if hasDiagnostic(rep, "old-slug.md", CategorySlugMismatch) {
		t.Errorf("expected no slug-mismatch when old slug is in synonyms; diagnostics: %+v", rep.Diagnostics)
	}
}

func TestLintDanglingRelation(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "dangling", `id: 01ABC
scope: personal
type: topic
title: Dangling
confidence: tentative
relations:
  - op: "@supersedes"
    target: "does-not-exist"
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "dangling.md", CategoryDanglingRelation)
	if d.Severity != SeverityError {
		t.Errorf("dangling-relation must be error, got %q", d.Severity)
	}
}

func TestLintResolvesRelationByIDSlugAndSynonym(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "alpha", `id: 01ALPHA
scope: personal
type: topic
title: Alpha
synonyms: ["alpha-aka"]
confidence: tentative
`, "body")
	// Three sources, each targeting alpha a different way.
	writeFixture(t, notes, "via-id", `id: 01VIAID
scope: personal
type: topic
title: Via ID
confidence: tentative
relations:
  - op: "@relates_to"
    target: "01ALPHA"
`, "body")
	writeFixture(t, notes, "via-slug", `id: 01VIASLUG
scope: personal
type: topic
title: Via Slug
confidence: tentative
relations:
  - op: "@relates_to"
    target: "alpha"
`, "body")
	writeFixture(t, notes, "via-synonym", `id: 01VIASYN
scope: personal
type: topic
title: Via Synonym
confidence: tentative
relations:
  - op: "@relates_to"
    target: "alpha-aka"
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	for _, name := range []string{"via-id.md", "via-slug.md", "via-synonym.md"} {
		if hasDiagnostic(rep, name, CategoryDanglingRelation) {
			t.Errorf("%s: relation should resolve, got dangling diagnostic", name)
		}
	}
}

func TestLintDetectsSupersedesCycle(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "a", `id: 01A
scope: personal
type: topic
title: A
confidence: tentative
relations:
  - op: "@supersedes"
    target: "b"
`, "body")
	writeFixture(t, notes, "b", `id: 01B
scope: personal
type: topic
title: B
confidence: tentative
relations:
  - op: "@supersedes"
    target: "a"
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	cycleCount := 0
	for _, d := range rep.Diagnostics {
		if d.Category == CategoryCycle {
			cycleCount++
		}
	}
	if cycleCount == 0 {
		t.Errorf("expected at least one cycle diagnostic, got 0: %+v", rep.Diagnostics)
	}
}

func TestLintDuplicateIDs(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "first", `id: 01SAME
scope: personal
type: topic
title: First
confidence: tentative
`, "body")
	writeFixture(t, notes, "second", `id: 01SAME
scope: personal
type: topic
title: Second
confidence: tentative
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	dupCount := 0
	for _, d := range rep.Diagnostics {
		if d.Category == CategoryDuplicateID {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Errorf("duplicate-id count = %d, want 1; diagnostics: %+v", dupCount, rep.Diagnostics)
	}
}

func TestLintUnknownOperator(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	writeFixture(t, notes, "alpha", `id: 01ALPHA
scope: personal
type: topic
title: Alpha
confidence: tentative
`, "body")
	writeFixture(t, notes, "uses-unknown", `id: 01UNKNOWN
scope: personal
type: topic
title: Uses unknown
confidence: tentative
relations:
  - op: "@not_a_spec_op"
    target: "alpha"
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "uses-unknown.md", CategoryUnknownOperator)
	if d.Severity != SeverityWarn {
		t.Errorf("unknown-operator must be warn, got %q", d.Severity)
	}
}

func TestLintMissingRecommendedConfidenceIsFixable(t *testing.T) {
	layer, notes := setupLintLayer(t, "personal")
	path := writeFixture(t, notes, "no-confidence", `id: 01ABC
scope: personal
type: topic
title: No confidence
`, "body")

	rep, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	d := findDiagnostic(t, rep, "no-confidence.md", CategoryMissingRecommended)
	if !d.Fixable {
		t.Error("missing-recommended/confidence must be marked fixable")
	}

	// Now run with --fix.
	rep2, err := Lint([]Layer{layer}, LintOptions{Fix: true})
	if err != nil {
		t.Fatalf("Lint --fix: %v", err)
	}
	if len(rep2.Fixed) != 1 {
		t.Fatalf("Fixed len = %d, want 1: %+v", len(rep2.Fixed), rep2.Fixed)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "confidence: tentative") {
		t.Errorf("--fix did not insert confidence: got %s", content)
	}

	// Re-lint must now be clean for this topic.
	rep3, err := Lint([]Layer{layer}, LintOptions{})
	if err != nil {
		t.Fatalf("Lint after fix: %v", err)
	}
	if hasDiagnostic(rep3, "no-confidence.md", CategoryMissingRecommended) {
		t.Error("missing-recommended must not re-fire after --fix")
	}
}

func TestLintScopeFilter(t *testing.T) {
	personal, personalNotes := setupLintLayer(t, "personal")
	project, projectNotes := setupLintLayer(t, "project")

	writeFixture(t, personalNotes, "p-bad", `id: 01P
scope: personal
type: topic
title: P bad
confidence: maybe
`, "body")
	writeFixture(t, projectNotes, "j-bad", `id: 01J
scope: project
type: topic
title: J bad
confidence: maybe
`, "body")

	rep, err := Lint([]Layer{personal, project}, LintOptions{Scope: "personal"})
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if rep.FilesWalked != 1 {
		t.Errorf("FilesWalked = %d, want 1 (scope filter)", rep.FilesWalked)
	}
	if hasDiagnostic(rep, "j-bad.md", CategoryInvalidEnum) {
		t.Errorf("project-layer findings must be excluded when --scope=personal")
	}
}
