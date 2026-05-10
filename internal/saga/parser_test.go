package saga

import (
	"strings"
	"testing"
)

func TestParseTopic_basic(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: project:acme-platform
type: topic
title: MJPEG performance
synonyms:
  - mjpeg slow
  - stream lento
sensitivity: internal
confidence: validated
created_at: 2026-04-12T10:30:00Z
updated_at: 2026-04-20T15:45:00Z
created_by: jorge@example.com
references:
  - path: controllers/stream.go
    lines: "120-180"
    blame_hash: a3f7d2c8e1b9
---

## Sumário
MJPEG é servido por handler dedicado, separado do go2rtc.
`)
	topic, err := ParseTopic(src)
	if err != nil {
		t.Fatalf("ParseTopic: %v", err)
	}
	if topic.Title != "MJPEG performance" {
		t.Errorf("title = %q", topic.Title)
	}
	if topic.Scope != "project:acme-platform" {
		t.Errorf("scope = %q", topic.Scope)
	}
	if got, want := len(topic.Synonyms), 2; got != want {
		t.Errorf("synonyms len = %d, want %d", got, want)
	}
	if got, want := len(topic.References), 1; got != want {
		t.Errorf("references len = %d, want %d", got, want)
	}
	if topic.References[0].BlameHash != "a3f7d2c8e1b9" {
		t.Errorf("blame_hash = %q", topic.References[0].BlameHash)
	}
	if !strings.Contains(topic.Body, "MJPEG é servido") {
		t.Errorf("body = %q", topic.Body)
	}
}

func TestParseTopic_missingFrontmatter(t *testing.T) {
	if _, err := ParseTopic([]byte("just markdown\n")); err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseTopic_unterminatedFrontmatter(t *testing.T) {
	if _, err := ParseTopic([]byte("---\nid: x\nscope: y\n")); err == nil {
		t.Fatal("expected error for unterminated frontmatter")
	}
}

func TestParseTopic_missingRequired(t *testing.T) {
	src := []byte(`---
title: only title
---

body
`)
	if _, err := ParseTopic(src); err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestParseTopic_invalidType(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: garbage
title: t
---

body
`)
	if _, err := ParseTopic(src); err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestParseTopic_acceptsAllSpecTypes(t *testing.T) {
	for _, typ := range SpecTypesAll() {
		src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: ` + typ + `
title: t
---

body
`)
		if _, err := ParseTopic(src); err != nil {
			t.Errorf("ParseTopic for type %q: %v", typ, err)
		}
	}
}

func TestParseTopic_relationsAllOperators(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: topic
title: with all operators
relations:
  - { op: "@supersedes",     target: "old-foo" }
  - { op: "@deprecated",     target: "soon-gone" }
  - { op: "@derived_from",   target: "investigation-x", note: "consolidated" }
  - { op: "@conflicts_with", target: "rival-view" }
  - { op: "@relates_to",     target: "neighbour" }
  - { op: "@refines",        target: "parent-fact" }
---

body
`)
	topic, err := ParseTopic(src)
	if err != nil {
		t.Fatalf("ParseTopic: %v", err)
	}
	if got, want := len(topic.Relations), 6; got != want {
		t.Fatalf("relations len = %d, want %d", got, want)
	}
	if len(topic.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", topic.Warnings)
	}
	if topic.Relations[2].Note != "consolidated" {
		t.Errorf("note not preserved: got %q", topic.Relations[2].Note)
	}
}

func TestParseTopic_relationsUnknownOperator(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: topic
title: unknown op
relations:
  - { op: "@invented", target: "future-target" }
---

body
`)
	topic, err := ParseTopic(src)
	if err != nil {
		t.Fatalf("ParseTopic: %v (unknown op should warn, not error)", err)
	}
	if got, want := len(topic.Relations), 1; got != want {
		t.Fatalf("relations len = %d, want %d (unknown ops are kept)", got, want)
	}
	if len(topic.Warnings) == 0 {
		t.Fatal("expected a warning for unknown operator")
	}
}

func TestParseTopic_relationsMissingOp(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: topic
title: no op
relations:
  - { target: "lonely-target" }
---

body
`)
	if _, err := ParseTopic(src); err == nil {
		t.Fatal("expected error for relation missing op")
	}
}

func TestParseTopic_relationsMissingTarget(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: topic
title: no target
relations:
  - { op: "@supersedes" }
---

body
`)
	if _, err := ParseTopic(src); err == nil {
		t.Fatal("expected error for relation missing target")
	}
}

func TestParseTopic_relationsAbsentField(t *testing.T) {
	src := []byte(`---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: personal
type: topic
title: no relations key at all
---

body
`)
	topic, err := ParseTopic(src)
	if err != nil {
		t.Fatalf("ParseTopic: %v", err)
	}
	if topic.Relations != nil {
		t.Errorf("expected nil Relations, got %v", topic.Relations)
	}
}
