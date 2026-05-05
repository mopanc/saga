package saga

import (
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"MJPEG performance", "mjpeg-performance"},
		{"Memória", "memoria"},
		{"memória de longo prazo", "memoria-de-longo-prazo"},
		{"go2rtc-arch!", "go2rtc-arch"},
		{"   trim me   ", "trim-me"},
		{"", "untitled"},
		{"!!!", "untitled"},
		{"___under_scores___", "under-scores"},
		{"Mr. O'Reilly", "mr-o-reilly"},
	}
	for _, tc := range cases {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRender_roundtrip(t *testing.T) {
	original := &Topic{
		ID:          "01HXY5KZQVJ8M3R7ABCDEFGHIJ",
		Scope:       "personal",
		Type:        "topic",
		Title:       "Test note",
		Synonyms:    []string{"test", "note"},
		Sensitivity: "internal",
		Confidence:  "proposed",
		CreatedAt:   time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
		References: []TopicReference{
			{Path: "src/foo.go", Lines: "10-20", BlameHash: "abc123"},
		},
		Body: "Body content here.\n\n## Section\n\nMore content.",
	}
	rendered, err := original.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(string(rendered), "---\n") {
		t.Errorf("missing opening ---:\n%s", rendered)
	}
	parsed, err := ParseTopic(rendered)
	if err != nil {
		t.Fatalf("re-parse: %v\nrendered:\n%s", err, rendered)
	}
	if parsed.ID != original.ID {
		t.Errorf("ID: got %q, want %q", parsed.ID, original.ID)
	}
	if parsed.Title != original.Title {
		t.Errorf("Title: got %q, want %q", parsed.Title, original.Title)
	}
	if parsed.Body != original.Body {
		t.Errorf("Body roundtrip mismatch:\ngot:  %q\nwant: %q", parsed.Body, original.Body)
	}
	if len(parsed.Synonyms) != 2 {
		t.Errorf("Synonyms: %v", parsed.Synonyms)
	}
	if len(parsed.References) != 1 || parsed.References[0].Path != "src/foo.go" {
		t.Errorf("References: %v", parsed.References)
	}
}

func TestRender_omitsEmptyOptional(t *testing.T) {
	t1 := &Topic{
		ID:          "01HXY5KZQVJ8M3R7ABCDEFGHIJ",
		Scope:       "personal",
		Type:        "topic",
		Title:       "Minimal",
		Sensitivity: "internal",
		Confidence:  "proposed",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		Body:        "x",
	}
	rendered, err := t1.Render()
	if err != nil {
		t.Fatal(err)
	}
	s := string(rendered)
	// omitempty fields should not appear when empty
	for _, field := range []string{"synonyms:", "references:", "related:", "created_by:", "updated_by:"} {
		if strings.Contains(s, field) {
			t.Errorf("rendered output contains optional empty field %q:\n%s", field, s)
		}
	}
}
