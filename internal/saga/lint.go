package saga

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Severity classifies a lint finding. Errors are spec violations; warnings are
// drift that doesn't break the spec but signals rot (slug ↔ title divergence,
// missing recommended fields, etc.).
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
)

// Diagnostic categories — stable JSON values; tools consume these to filter
// or aggregate. New categories append; existing ones never rename.
const (
	CategoryParse              = "parse-error"
	CategoryMissingField       = "missing-field"
	CategoryInvalidType        = "invalid-type"
	CategoryInvalidEnum        = "invalid-enum"
	CategoryScopeMismatch      = "scope-mismatch"
	CategoryUnknownOperator    = "unknown-operator"
	CategoryDanglingRelation   = "dangling-relation"
	CategoryCycle              = "cycle"
	CategorySlugMismatch       = "slug-mismatch"
	CategoryDuplicateID        = "duplicate-id"
	CategoryMissingRecommended = "missing-recommended"
)

// Diagnostic — one finding from `saga lint`. JSON shape is stable.
type Diagnostic struct {
	FilePath string   `json:"file_path"`
	TopicID  string   `json:"topic_id,omitempty"`
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
	Fixable  bool     `json:"fixable,omitempty"`
}

// FixedTopic — a topic that --fix touched, with a list of human-readable
// changes (no diff format; the file on disk is the source of truth post-fix).
type FixedTopic struct {
	FilePath string   `json:"file_path"`
	TopicID  string   `json:"topic_id"`
	Changes  []string `json:"changes"`
}

// LintReport — aggregate result. JSON shape is stable.
type LintReport struct {
	FilesWalked int          `json:"files_walked"`
	ParseErrors int          `json:"parse_errors"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Fixed       []FixedTopic `json:"fixed,omitempty"`
}

// HasFindings reports whether the report contains any diagnostic (parse error
// or rule violation). Used to choose exit code 0 vs 1.
func (r *LintReport) HasFindings() bool {
	return len(r.Diagnostics) > 0
}

// LintOptions toggles linter behaviour.
type LintOptions struct {
	// Scope restricts the walk to a single layer scope (e.g. "personal"). Empty
	// scope walks every active layer.
	Scope string
	// Fix applies safe auto-fixes: insert missing recommended defaults
	// (currently only `confidence: tentative` per spec §5.1). Never edits body
	// content. Never touches relations.
	Fix bool
}

// Allowed-value tables for enum traits — single source of truth, mirror of
// spec §2.2 and §5. Sorted for deterministic diagnostic messages.
var (
	allowedConfidence      = []string{"canonical", "tentative", "proposed"}
	allowedLifecycle       = []string{"durable", "volatile", "archived"}
	allowedProvenance      = []string{"human_generated", "agent_generated", "derived"}
	allowedMemoryFamily    = []string{"declarative", "procedural", "episodic"}
	allowedOperatorSurface = []string{"inert", "executable"}
	allowedSensitivity     = []string{"public", "internal", "confidential"}
)

// topicRecord is the in-memory view used by validation. Holds enough to check
// cross-references (relation targets) without re-reading from disk.
type topicRecord struct {
	Topic     *Topic
	FilePath  string
	LayerRoot string
	LayerMeta Meta
	// Slug derived from filename (basename without .md).
	FilenameSlug string
}

// Lint validates every topic in the given layers against spec v1.0 and returns
// a structured report. Mutations to disk only happen when opts.Fix is true.
//
// The walk is two-pass: pass 1 reads and parses every file (collecting parse
// errors as fatal-category diagnostics); pass 2 runs validation rules that
// need the global index (relation target resolution, duplicate id detection,
// cycle detection).
func Lint(layers []Layer, opts LintOptions) (*LintReport, error) {
	rep := &LintReport{}

	// Pass 1 — walk and parse.
	records := make([]*topicRecord, 0, 64)
	for _, layer := range layers {
		if opts.Scope != "" && layer.Scope != opts.Scope {
			continue
		}
		walkErr := filepath.WalkDir(layer.NotesDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rep.FilesWalked++

			content, rerr := os.ReadFile(path) // #nosec G304 -- path is rooted at a resolved layer notes dir
			if rerr != nil {
				rep.ParseErrors++
				rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
					FilePath: path,
					Severity: SeverityError,
					Category: CategoryParse,
					Message:  fmt.Sprintf("read failed: %v", rerr),
				})
				return nil
			}
			topic, perr := ParseTopic(content)
			if perr != nil {
				rep.ParseErrors++
				rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
					FilePath: path,
					Severity: SeverityError,
					Category: classifyParseError(perr),
					Message:  perr.Error(),
				})
				return nil
			}
			records = append(records, &topicRecord{
				Topic:        topic,
				FilePath:     path,
				LayerRoot:    layer.RootPath,
				LayerMeta:    layer.Meta,
				FilenameSlug: strings.TrimSuffix(d.Name(), ".md"),
			})
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, fs.ErrNotExist) {
			return nil, fmt.Errorf("walk %s: %w", layer.NotesDir, walkErr)
		}
	}

	// Pass 2 — per-record validation + cross-record checks.
	idIndex := buildIDIndex(records)
	slugIndex := buildSlugIndex(records)
	synonymIndex := buildSynonymIndex(records)

	for _, rec := range records {
		validateRecord(rec, idIndex, slugIndex, synonymIndex, rep)
	}
	detectDuplicateIDs(records, rep)
	detectRelationCycles(records, idIndex, slugIndex, synonymIndex, rep)

	// Pass 3 — optional fixes. Only safe insertions; never touches body or
	// relations. Writes are atomic.
	if opts.Fix {
		if err := applyFixes(records, rep); err != nil {
			return rep, err
		}
	}

	// Stable order for deterministic output (and easier diff in tests).
	sort.SliceStable(rep.Diagnostics, func(i, j int) bool {
		if rep.Diagnostics[i].FilePath != rep.Diagnostics[j].FilePath {
			return rep.Diagnostics[i].FilePath < rep.Diagnostics[j].FilePath
		}
		return rep.Diagnostics[i].Category < rep.Diagnostics[j].Category
	})

	return rep, nil
}

func classifyParseError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "frontmatter missing required field"):
		return CategoryMissingField
	case strings.Contains(msg, "invalid type"):
		return CategoryInvalidType
	default:
		return CategoryParse
	}
}

func buildIDIndex(records []*topicRecord) map[string]*topicRecord {
	idx := make(map[string]*topicRecord, len(records))
	for _, r := range records {
		// On collision pass 2 emits a duplicate-id diagnostic; keep the first.
		if _, exists := idx[r.Topic.ID]; !exists {
			idx[r.Topic.ID] = r
		}
	}
	return idx
}

func buildSlugIndex(records []*topicRecord) map[string]*topicRecord {
	idx := make(map[string]*topicRecord, len(records))
	for _, r := range records {
		key := scopeQualified(r.Topic.Scope, r.FilenameSlug)
		idx[key] = r
		idx[r.FilenameSlug] = r // unscoped fallback (last writer wins; cross-scope ambiguity is engine-choice per spec §9)
	}
	return idx
}

func buildSynonymIndex(records []*topicRecord) map[string]*topicRecord {
	idx := make(map[string]*topicRecord)
	for _, r := range records {
		for _, syn := range r.Topic.Synonyms {
			s := Slugify(syn)
			if s == "" {
				continue
			}
			if _, exists := idx[s]; !exists {
				idx[s] = r
			}
		}
	}
	return idx
}

func scopeQualified(scope, slug string) string {
	return scope + ":" + slug
}

func validateRecord(rec *topicRecord, idIndex, slugIndex, synonymIndex map[string]*topicRecord, rep *LintReport) {
	t := rec.Topic

	// Spec §2.1 — scope MUST match the containing layer's meta.yml.
	if t.Scope != rec.LayerMeta.Scope {
		rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
			FilePath: rec.FilePath,
			TopicID:  t.ID,
			Severity: SeverityError,
			Category: CategoryScopeMismatch,
			Field:    "scope",
			Message:  fmt.Sprintf("frontmatter scope=%q but layer scope=%q", t.Scope, rec.LayerMeta.Scope),
		})
	}

	// Spec §5 — trait enums.
	checkEnum(rep, rec, "confidence", t.Confidence, allowedConfidence, true)
	checkEnum(rep, rec, "lifecycle", lookupField(rec, "lifecycle"), allowedLifecycle, false)
	checkEnum(rep, rec, "provenance", lookupField(rec, "provenance"), allowedProvenance, false)
	checkEnum(rep, rec, "memory_family", lookupField(rec, "memory_family"), allowedMemoryFamily, false)
	checkEnum(rep, rec, "operator_surface", lookupField(rec, "operator_surface"), allowedOperatorSurface, false)
	checkEnum(rep, rec, "sensitivity", t.Sensitivity, allowedSensitivity, false)

	// Spec §1.1 — slug should match filename. Soft warning: titles legitimately
	// drift over time. We only flag when both filename slug and title-derived
	// slug are non-empty and disagree, and the filename slug isn't already in
	// synonyms.
	if titleSlug := Slugify(t.Title); titleSlug != "" && rec.FilenameSlug != "" && titleSlug != rec.FilenameSlug {
		if !slugInSynonyms(rec.FilenameSlug, t.Synonyms) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				FilePath: rec.FilePath,
				TopicID:  t.ID,
				Severity: SeverityWarn,
				Category: CategorySlugMismatch,
				Field:    "title",
				Message:  fmt.Sprintf("filename slug %q does not match title-derived slug %q; rename or add the previous slug to synonyms", rec.FilenameSlug, titleSlug),
			})
		}
	}

	// Spec §6.1 — dangling relation targets MUST be surfaced (at minimum by lint).
	for i, r := range t.Relations {
		if r.Op == "" || r.Target == "" {
			// Parser already rejects these; defensive guard for direct callers.
			continue
		}
		if !KnownOperators[r.Op] {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				FilePath: rec.FilePath,
				TopicID:  t.ID,
				Severity: SeverityWarn,
				Category: CategoryUnknownOperator,
				Field:    fmt.Sprintf("relations[%d].op", i),
				Message:  fmt.Sprintf("operator %q is not in the spec §6.2 pure-metadata set; accepted as opaque", r.Op),
			})
		}
		if !resolveRelationTarget(r.Target, t.Scope, idIndex, slugIndex, synonymIndex) {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				FilePath: rec.FilePath,
				TopicID:  t.ID,
				Severity: SeverityError,
				Category: CategoryDanglingRelation,
				Field:    fmt.Sprintf("relations[%d].target", i),
				Message:  fmt.Sprintf("%s target %q does not resolve to any topic in active layers", r.Op, r.Target),
			})
		}
	}

	// Spec §2.2 — recommended frontmatter. Missing `confidence` is the only
	// one we currently surface (and the only one --fix touches).
	if t.Confidence == "" {
		rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
			FilePath: rec.FilePath,
			TopicID:  t.ID,
			Severity: SeverityWarn,
			Category: CategoryMissingRecommended,
			Field:    "confidence",
			Message:  "recommended field `confidence` missing; spec §5.1 default is `tentative`",
			Fixable:  true,
		})
	}
}

func checkEnum(rep *LintReport, rec *topicRecord, field, value string, allowed []string, required bool) {
	if value == "" {
		// Required-field absence is handled by the parser; recommended-field
		// absence is handled by the `missing-recommended` rule above (for the
		// fields we surface). No diagnostic here.
		_ = required
		return
	}
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
		FilePath: rec.FilePath,
		TopicID:  rec.Topic.ID,
		Severity: SeverityError,
		Category: CategoryInvalidEnum,
		Field:    field,
		Message:  fmt.Sprintf("%s=%q is not one of %v", field, value, allowed),
	})
}

// lookupField returns the value of a frontmatter field that is not modelled on
// the Topic struct. Reads the raw file again — cheap and avoids polluting the
// parser with v2 fields.
func lookupField(rec *topicRecord, field string) string {
	content, err := os.ReadFile(rec.FilePath) // #nosec G304 -- path rooted at resolved notes dir
	if err != nil {
		return ""
	}
	// Crude line scan over the frontmatter block. YAML parsing is overkill for
	// a single scalar and would force a new struct layout.
	const delim = "---"
	lines := strings.Split(string(content), "\n")
	if len(lines) == 0 || lines[0] != delim {
		return ""
	}
	prefix := field + ":"
	for _, line := range lines[1:] {
		if line == delim {
			break
		}
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func slugInSynonyms(slug string, synonyms []string) bool {
	for _, s := range synonyms {
		if Slugify(s) == slug {
			return true
		}
	}
	return false
}

// resolveRelationTarget mirrors spec §9.2 resolution order in the subset that
// makes sense at lint time (no MCP-driven disambiguation).
func resolveRelationTarget(target, sourceScope string, idIndex, slugIndex, synonymIndex map[string]*topicRecord) bool {
	if target == "" {
		return false
	}
	// Fully qualified scope:slug
	if strings.Contains(target, ":") {
		if _, ok := slugIndex[target]; ok {
			return true
		}
	}
	if _, ok := idIndex[target]; ok {
		return true
	}
	if _, ok := slugIndex[scopeQualified(sourceScope, target)]; ok {
		return true
	}
	if _, ok := slugIndex[target]; ok {
		return true
	}
	if _, ok := synonymIndex[Slugify(target)]; ok {
		return true
	}
	return false
}

func detectDuplicateIDs(records []*topicRecord, rep *LintReport) {
	seen := make(map[string]*topicRecord, len(records))
	for _, r := range records {
		if prev, ok := seen[r.Topic.ID]; ok {
			rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
				FilePath: r.FilePath,
				TopicID:  r.Topic.ID,
				Severity: SeverityError,
				Category: CategoryDuplicateID,
				Field:    "id",
				Message:  fmt.Sprintf("duplicate topic id %q — also at %s", r.Topic.ID, prev.FilePath),
			})
			continue
		}
		seen[r.Topic.ID] = r
	}
}

// detectRelationCycles flags cycles in @supersedes and @derived_from graphs.
// Spec §6.3 mandates engines refuse them on write; lint catches retroactive
// drift in case content was edited outside the engine.
func detectRelationCycles(records []*topicRecord, idIndex, slugIndex, synonymIndex map[string]*topicRecord, rep *LintReport) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	check := func(op string) {
		// Build adjacency on the subset of records whose outgoing edge uses op.
		adj := make(map[*topicRecord][]*topicRecord, len(records))
		for _, r := range records {
			for _, rel := range r.Topic.Relations {
				if rel.Op != op {
					continue
				}
				target := lookupTarget(rel.Target, r.Topic.Scope, idIndex, slugIndex, synonymIndex)
				if target == nil {
					continue
				}
				adj[r] = append(adj[r], target)
			}
		}
		color := make(map[*topicRecord]int, len(records))
		var dfs func(n *topicRecord) *topicRecord
		dfs = func(n *topicRecord) *topicRecord {
			color[n] = gray
			for _, next := range adj[n] {
				switch color[next] {
				case gray:
					return next
				case white:
					if c := dfs(next); c != nil {
						return c
					}
				}
			}
			color[n] = black
			return nil
		}
		for _, r := range records {
			if color[r] != white {
				continue
			}
			if cycleStart := dfs(r); cycleStart != nil {
				rep.Diagnostics = append(rep.Diagnostics, Diagnostic{
					FilePath: r.FilePath,
					TopicID:  r.Topic.ID,
					Severity: SeverityError,
					Category: CategoryCycle,
					Field:    "relations",
					Message:  fmt.Sprintf("%s cycle detected (reaches topic %s)", op, cycleStart.Topic.ID),
				})
			}
		}
	}
	check("@supersedes")
	check("@derived_from")
}

func lookupTarget(target, sourceScope string, idIndex, slugIndex, synonymIndex map[string]*topicRecord) *topicRecord {
	if target == "" {
		return nil
	}
	if strings.Contains(target, ":") {
		if r, ok := slugIndex[target]; ok {
			return r
		}
	}
	if r, ok := idIndex[target]; ok {
		return r
	}
	if r, ok := slugIndex[scopeQualified(sourceScope, target)]; ok {
		return r
	}
	if r, ok := slugIndex[target]; ok {
		return r
	}
	if r, ok := synonymIndex[Slugify(target)]; ok {
		return r
	}
	return nil
}

// applyFixes — v1 scope: only safe insertions on YAML frontmatter scalars.
// Currently fixable: missing `confidence: tentative`. Never edits body, never
// touches relations.
func applyFixes(records []*topicRecord, rep *LintReport) error {
	byPath := make(map[string][]string, len(rep.Diagnostics))
	for _, d := range rep.Diagnostics {
		if !d.Fixable {
			continue
		}
		byPath[d.FilePath] = append(byPath[d.FilePath], d.Field)
	}
	for _, rec := range records {
		fields, ok := byPath[rec.FilePath]
		if !ok {
			continue
		}
		var changes []string
		for _, f := range fields {
			if f == "confidence" && rec.Topic.Confidence == "" {
				rec.Topic.Confidence = "tentative"
				changes = append(changes, "inserted `confidence: tentative` (spec §5.1 default)")
			}
		}
		if len(changes) == 0 {
			continue
		}
		rendered, err := rec.Topic.Render()
		if err != nil {
			return fmt.Errorf("render %s: %w", rec.FilePath, err)
		}
		if err := writeFileAtomic(rec.FilePath, rendered, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", rec.FilePath, err)
		}
		rep.Fixed = append(rep.Fixed, FixedTopic{
			FilePath: rec.FilePath,
			TopicID:  rec.Topic.ID,
			Changes:  changes,
		})
	}
	return nil
}
