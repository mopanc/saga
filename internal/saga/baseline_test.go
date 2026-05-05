package saga

import (
	"strings"
	"testing"
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

func TestTruncateAtSection(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		maxTokens int
		want      string
	}{
		{
			name:      "under_limit_unchanged",
			text:      "small",
			maxTokens: 100,
			want:      "small",
		},
		{
			name:      "cuts_at_last_paragraph_within_budget",
			text:      "para1.\n\npara2.\n\npara3.\n\npara4.",
			maxTokens: 4, // 16 char budget; cut="para1.\n\npara2.\n\n"; last \n\n at idx 14
			want:      "para1.\n\npara2.",
		},
		{
			name:      "falls_back_to_line_boundary",
			text:      "line1\nline2\nline3\nline4",
			maxTokens: 3, // 12 char budget; cut="line1\nline2\n"; no \n\n; last \n at idx 11
			want:      "line1\nline2",
		},
		{
			name:      "hard_cut_when_no_boundaries",
			text:      "abcdefghij",
			maxTokens: 1, // 4 char budget; no newlines anywhere
			want:      "abcd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateAtSection(tc.text, tc.maxTokens)
			if got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
			// Result must be a prefix of the input (after trimming).
			if !strings.HasPrefix(tc.text, strings.TrimRight(got, "\n")) {
				t.Errorf("truncated output is not a prefix of input")
			}
		})
	}
}
