package main

import (
	"bytes"
	"strings"
	"testing"

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
	emitLensBlock(&buf, cfg, 3, "identity body here", results)

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
}

func TestEmitLensBlock_emptyResultsSkipsContextBlock(t *testing.T) {
	cfg := &saga.Config{DBPath: "/tmp/saga.db"}
	var buf bytes.Buffer
	emitLensBlock(&buf, cfg, 0, "", nil)
	if strings.Contains(buf.String(), "<saga-context>") {
		t.Fatalf("did not expect <saga-context> for empty results, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "<saga-identity>") {
		t.Fatalf("did not expect <saga-identity> for empty baseline, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "<saga-meta>") {
		t.Fatalf("expected <saga-meta> always, got %q", buf.String())
	}
}
