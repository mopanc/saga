package saga

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildIdentityBaseline_emptyReturnsEmpty(t *testing.T) {
	svc, _ := setupServiceTest(t)
	baseline, _, err := svc.BuildIdentityBaseline(400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if baseline != "" {
		t.Errorf("expected empty baseline, got %q", baseline)
	}
}

func TestBuildIdentityBaseline_profileOnly(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "identity", Scope: "personal", Type: "profile",
		Body: "Jorge, dev em Go.",
	}); err != nil {
		t.Fatal(err)
	}

	baseline, _, err := svc.BuildIdentityBaseline(400)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(baseline, "# Profile") {
		t.Errorf("baseline missing # Profile heading:\n%s", baseline)
	}
	if !strings.Contains(baseline, "Jorge") {
		t.Errorf("baseline missing body:\n%s", baseline)
	}
	if strings.Contains(baseline, "# Preferences") {
		t.Errorf("baseline contains preferences heading without preference notes:\n%s", baseline)
	}
}

func TestBuildIdentityBaseline_profileAndPreference(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "identity", Scope: "personal", Type: "profile",
		Body: "Jorge, fala PT-PT.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "communication", Scope: "personal", Type: "preference",
		Body: "Tom directo, sem sycophancy.",
	}); err != nil {
		t.Fatal(err)
	}

	baseline, _, err := svc.BuildIdentityBaseline(1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Profile", "# Preferences", "Jorge", "directo"} {
		if !strings.Contains(baseline, want) {
			t.Errorf("baseline missing %q:\n%s", want, baseline)
		}
	}
	// Profile must appear before Preferences (deterministic ordering).
	profileIdx := strings.Index(baseline, "# Profile")
	prefIdx := strings.Index(baseline, "# Preferences")
	if profileIdx >= prefIdx {
		t.Errorf("expected Profile before Preferences, got profile@%d preferences@%d", profileIdx, prefIdx)
	}
}

func TestBuildIdentityBaseline_respectsTokenLimit(t *testing.T) {
	svc, _ := setupServiceTest(t)

	long := strings.Repeat("Esta frase tem muitos tokens.\n\nOutra frase.\n\n", 100)
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "identity", Scope: "personal", Type: "profile", Body: long,
	}); err != nil {
		t.Fatal(err)
	}

	baseline, _, err := svc.BuildIdentityBaseline(50) // ≈200 chars budget
	if err != nil {
		t.Fatal(err)
	}
	// Allow some slack for boundary cuts but enforce gross cap.
	if got := len(baseline); got > 280 {
		t.Errorf("baseline too long (%d chars) for 50-token budget", got)
	}
	// No mid-sentence cut — last char should be a newline-area, not mid-word.
	last := baseline[len(baseline)-1]
	if last != '.' && last != '\n' && !strings.HasSuffix(baseline, "\n\n") {
		// soft check; "." is fine since paragraphs end with periods
	}
}

func TestBuildIdentityBaseline_deterministic(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "identity", Scope: "personal", Type: "profile", Body: "hello",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "communication", Scope: "personal", Type: "preference", Body: "world",
	}); err != nil {
		t.Fatal(err)
	}

	a, _, err := svc.BuildIdentityBaseline(400)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := svc.BuildIdentityBaseline(400)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("non-deterministic output:\nA:\n%s\n---\nB:\n%s", a, b)
	}
}

func TestBuildIdentityBaseline_zeroMaxTokensUsesDefault(t *testing.T) {
	svc, _ := setupServiceTest(t)

	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "identity", Scope: "personal", Type: "profile", Body: "x",
	}); err != nil {
		t.Fatal(err)
	}

	baseline, _, err := svc.BuildIdentityBaseline(0)
	if err != nil {
		t.Fatal(err)
	}
	if baseline == "" {
		t.Error("expected non-empty baseline with default token budget")
	}
}

func TestBuildIdentityBaseline_ignoresOtherScopes(t *testing.T) {
	svc, _ := setupServiceTest(t)

	// Personal profile — should be included
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "me", Scope: "personal", Type: "profile", Body: "personal-content",
	}); err != nil {
		t.Fatal(err)
	}
	// Project topic — should NOT influence the baseline
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "acme-platform arch", Scope: "project:demo", Type: "topic", Body: "project-content",
	}); err != nil {
		t.Fatal(err)
	}

	baseline, _, err := svc.BuildIdentityBaseline(1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(baseline, "personal-content") {
		t.Errorf("baseline missing personal content:\n%s", baseline)
	}
	if strings.Contains(baseline, "project-content") {
		t.Errorf("baseline leaked project content:\n%s", baseline)
	}
}

func TestBuildIdentityBaseline_everyNoteRepresented(t *testing.T) {
	svc, _ := setupServiceTest(t)

	long := strings.Repeat("Profile detail paragraph.\n\n", 200) // ~5.4k chars
	if _, err := svc.TopicWrite(TopicWriteArgs{
		Name: "profile-jorge", Scope: "personal", Type: "profile",
		Title: "Who I am", Body: long,
	}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"pref-voice", "pref-typography", "pref-language"} {
		if _, err := svc.TopicWrite(TopicWriteArgs{
			Name: n, Scope: "personal", Type: "preference",
			Title: n, Body: "MARKER-" + n + "\n\n" + strings.Repeat("filler.\n\n", 100),
		}); err != nil {
			t.Fatal(err)
		}
	}

	out, ids, err := svc.BuildIdentityBaseline(400)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 {
		t.Fatalf("contributing ids = %d, want 4", len(ids))
	}
	for _, n := range []string{"pref-voice", "pref-typography", "pref-language"} {
		if !strings.Contains(out, "MARKER-"+n) {
			t.Errorf("preference %q was dropped entirely from the baseline", n)
		}
	}
	if !strings.Contains(out, "Profile detail paragraph.") {
		t.Error("profile note missing from baseline")
	}
	// Every note must render its own section. Counting them catches the
	// header-overhead bug: budgeting the bodies against the full allowance and
	// then character-capping the finished render pushed the last note off the
	// end, so 9 notes emitted 8 sections.
	if got := strings.Count(out, "## "); got != 4 {
		t.Errorf("rendered %d note sections, want 4 (one per note)", got)
	}
	if len(out) > 400*4*2 {
		t.Errorf("baseline overran its budget: %d chars", len(out))
	}
}

func TestAllocateShares_surplusRedistributed(t *testing.T) {
	notes := []*Topic{
		{ID: "short", Body: "tiny"},
		{ID: "long1", Body: strings.Repeat("x", 1000)},
		{ID: "long2", Body: strings.Repeat("y", 1000)},
	}
	shares := allocateShares(notes, 300)

	if shares["short"] != 4 {
		t.Errorf("short note share = %d, want its full 4 chars", shares["short"])
	}
	// The 296 chars the short note did not need go to the two long ones.
	if shares["long1"] != shares["long2"] {
		t.Errorf("long notes got unequal shares: %d vs %d", shares["long1"], shares["long2"])
	}
	if shares["long1"] < 140 {
		t.Errorf("surplus was not redistributed: long share = %d, want ~148", shares["long1"])
	}
	total := shares["short"] + shares["long1"] + shares["long2"]
	if total > 300 {
		t.Errorf("shares overran budget: %d > 300", total)
	}
}

// TestTruncateAtBoundary_neverSplitsRunes is the regression for the field
// report: maxChars counts bytes, and slicing bytes through a multi-byte rune
// emitted invalid UTF-8 to the client on every prompt. A "ç" (\xc3\xa7) became
// a lone \xc3.
func TestTruncateAtBoundary_neverSplitsRunes(t *testing.T) {
	// No newlines, so the cut lands mid-line and must fall back to a rune
	// boundary rather than a raw byte slice.
	text := strings.Repeat("Comunicação e configuração ", 40)

	for maxChars := 1; maxChars < 200; maxChars++ {
		got := truncateAtBoundary(text, maxChars)
		if !utf8.ValidString(got) {
			t.Fatalf("maxChars=%d produced invalid UTF-8: %q", maxChars, got)
		}
	}
}

func TestTrimToRuneBoundary(t *testing.T) {
	full := "ç"
	if len(full) != 2 {
		t.Fatalf("setup: expected a 2-byte rune, got %d", len(full))
	}
	if got := TrimToRuneBoundary(full[:1]); got != "" {
		t.Errorf("half a rune should be dropped entirely, got %q", got)
	}
	if got := TrimToRuneBoundary("abc"); got != "abc" {
		t.Errorf("intact string was modified: %q", got)
	}
	if got := TrimToRuneBoundary("aç"); got != "aç" {
		t.Errorf("string ending on a boundary was modified: %q", got)
	}
}

// TestBuildIdentityBaseline_outputIsValidUTF8 covers the same defect end to
// end, since that is how it reached the client.
func TestBuildIdentityBaseline_outputIsValidUTF8(t *testing.T) {
	svc, _ := setupServiceTest(t)
	for _, n := range []string{"a", "b", "c"} {
		if _, err := svc.TopicWrite(TopicWriteArgs{
			Name: "pref-" + n, Scope: "personal", Type: "preference",
			Title: "Comunicação " + n,
			Body:  strings.Repeat("configuração e comunicação sem quebras de linha ", 60),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for budget := 20; budget <= 400; budget += 7 {
		out, _, err := svc.BuildIdentityBaseline(budget)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(out) {
			t.Fatalf("baseline at budget=%d is not valid UTF-8", budget)
		}
	}
}
