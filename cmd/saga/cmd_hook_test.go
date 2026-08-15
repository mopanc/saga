package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mopanc/saga/internal/saga"
)

func TestTruncateTopicBody_underLimitUntouched(t *testing.T) {
	body := "short body content"
	got := truncateTopicBody(body, 100)
	if got != body {
		t.Fatalf("expected untouched body, got %q", got)
	}
}

func TestTruncateTopicBody_cutsAtParagraphBoundary(t *testing.T) {
	body := "first paragraph here\n\nsecond paragraph here\n\nthird paragraph here"
	got := truncateTopicBody(body, 30)
	if !strings.HasPrefix(got, "first paragraph here") {
		t.Fatalf("expected to keep first paragraph, got %q", got)
	}
	if strings.Contains(got, "second paragraph") {
		t.Fatalf("expected second paragraph dropped, got %q", got)
	}
	if !strings.Contains(got, "topic_read") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestTruncateTopicBody_cutsAtLineBoundaryWhenNoParagraph(t *testing.T) {
	body := "line one is here\nline two is here\nline three is here"
	got := truncateTopicBody(body, 20)
	if !strings.HasPrefix(got, "line one is here") {
		t.Fatalf("expected first line preserved, got %q", got)
	}
	if !strings.Contains(got, "topic_read") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
}

func TestTruncateTopicBody_hardCutWhenNoBoundary(t *testing.T) {
	body := strings.Repeat("a", 200)
	got := truncateTopicBody(body, 50)
	if !strings.Contains(got, "topic_read") {
		t.Fatalf("expected truncation marker, got %q", got)
	}
	// 50 chars + marker
	if len(got) > 50+len(truncationMarker) {
		t.Fatalf("expected hard cut at 50 chars + marker, got %d", len(got))
	}
}

func TestCapHookOutput_underLimitUntouched(t *testing.T) {
	out := []byte("small output\n")
	got := capHookOutput(out, 1024)
	if !bytes.Equal(got, out) {
		t.Fatalf("expected untouched, got %q", got)
	}
}

func TestCapHookOutput_cutsAndAnnotates(t *testing.T) {
	out := []byte(strings.Repeat("line of text here\n", 100))
	got := capHookOutput(out, 200)
	if len(got) > 400 {
		t.Fatalf("expected output close to cap, got %d bytes", len(got))
	}
	if !strings.Contains(string(got), "capped at 200 bytes") {
		t.Fatalf("expected cap marker, got %q", got)
	}
}

func TestEmitLensBlock_capsLargeBodies(t *testing.T) {
	cfg := &saga.Config{DBPath: "/tmp/saga.db"}
	// Three topics, each with a body that would individually exceed the
	// per-topic cap. Without truncation the block would be ~30KB; with
	// truncation it must fit comfortably.
	results := []saga.TopicSnippet{
		{Title: "topic-a", Scope: "personal", FilePath: "/nonexistent-a.md"},
		{Title: "topic-b", Scope: "personal", FilePath: "/nonexistent-b.md"},
		{Title: "topic-c", Scope: "personal", FilePath: "/nonexistent-c.md"},
	}
	var buf bytes.Buffer
	emitLensBlock(&buf, cfg, 3, "identity body here", "- **A rule** `[personal]`", results)

	if buf.Len() > maxHookOutputBytes {
		t.Fatalf("output exceeded hard cap: %d > %d", buf.Len(), maxHookOutputBytes)
	}
	got := buf.String()
	if !strings.Contains(got, "<saga-meta>") {
		t.Errorf("expected <saga-meta> block")
	}
	if !strings.Contains(got, "<saga-identity>") {
		t.Errorf("expected <saga-identity> block")
	}
	if !strings.Contains(got, "<saga-context>") {
		t.Errorf("expected <saga-context> block")
	}
	if !strings.Contains(got, "<saga-rules>") {
		t.Errorf("expected <saga-rules> block")
	}
}

func TestEmitLensBlock_emptyResultsSkipsContextBlock(t *testing.T) {
	cfg := &saga.Config{DBPath: "/tmp/saga.db"}
	var buf bytes.Buffer
	emitLensBlock(&buf, cfg, 0, "", "", nil)
	if strings.Contains(buf.String(), "<saga-context>") {
		t.Fatalf("did not expect <saga-context> for empty results, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "<saga-identity>") {
		t.Fatalf("did not expect <saga-identity> for empty baseline, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "<saga-rules>") {
		t.Fatalf("did not expect <saga-rules> for empty catalogue, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "<saga-meta>") {
		t.Fatalf("expected <saga-meta> always, got %q", buf.String())
	}
}

// TestCapHookOutput_closesTruncatedSections is the regression for the field
// report: a cut through <saga-context> emitted an opening tag with no closer,
// handing the client malformed markup on every capped prompt.
func TestCapHookOutput_closesTruncatedSections(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("<saga-meta>\nmeta\n</saga-meta>\n")
	buf.WriteString("<saga-context>\n")
	for i := 0; i < 500; i++ {
		buf.WriteString("a line of topic body that will be cut somewhere in here\n")
	}
	buf.WriteString("</saga-context>\n")

	const cap = 2048
	got := string(capHookOutput(buf.Bytes(), cap))

	if strings.Count(got, "<saga-context>") != strings.Count(got, "</saga-context>") {
		t.Errorf("<saga-context> left unclosed after truncation:\n%q", tail(got, 200))
	}
	if strings.Count(got, "<saga-meta>") != strings.Count(got, "</saga-meta>") {
		t.Error("<saga-meta> left unclosed after truncation")
	}
	if !strings.Contains(got, "capped at") {
		t.Error("truncation must be announced, not silent")
	}
	// The cap is a promise about the output, not about the part before the
	// notice: the field report measured 8250 bytes against an announced 8192.
	if len(got) > cap {
		t.Errorf("output overran the cap it announces: %d > %d", len(got), cap)
	}
}

func TestCapHookOutput_untouchedBelowCap(t *testing.T) {
	in := []byte("<saga-meta>\nsmall\n</saga-meta>\n")
	if got := capHookOutput(in, 8192); string(got) != string(in) {
		t.Errorf("output below the cap was modified: %q", got)
	}
}

func TestCapHookOutput_producesValidUTF8(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("<saga-context>\n")
	buf.WriteString(strings.Repeat("configuração e comunicação ", 200))
	buf.WriteString("\n</saga-context>\n")

	for cap := 200; cap < 1200; cap += 13 {
		got := capHookOutput(buf.Bytes(), cap)
		if !utf8.Valid(got) {
			t.Fatalf("cap=%d produced invalid UTF-8", cap)
		}
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
