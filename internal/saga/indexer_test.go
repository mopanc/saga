package saga

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleTopic = `---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ
scope: project:demo
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
    blame_hash: a3f7d2c8
---

## Sumário
MJPEG é servido por handler dedicado.
`

const sampleTopicTwo = `---
id: 01HXY5KZQVJ8M3R7ABCDEFXYZ2
scope: project:demo
type: topic
title: Socket protocol
sensitivity: internal
confidence: proposed
created_at: 2026-04-15T09:00:00Z
updated_at: 2026-04-15T09:00:00Z
---

Socket protocol notes.
`

func setupProjectLayer(t *testing.T, scope string, files map[string]string) Layer {
	t.Helper()
	root := t.TempDir()
	notes := filepath.Join(root, "topics")
	if err := os.MkdirAll(notes, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(notes, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Layer{
		Scope:    scope,
		RootPath: root,
		NotesDir: notes,
		Meta:     Meta{Scope: scope, NotesDir: "topics/"},
	}
}

func TestIndexLayer_basic(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"mjpeg-performance.md": sampleTopic,
		"socket-protocol.md":   sampleTopicTwo,
	})

	result, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatalf("IndexLayer: %v", err)
	}
	if result.Indexed != 2 {
		t.Errorf("Indexed = %d, want 2; errors: %+v", result.Indexed, result.Errors)
	}
	if result.Failed != 0 {
		t.Errorf("Failed = %d, want 0", result.Failed)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("topic_index count = %d, want 2", count)
	}

	// FTS5 row present and searchable
	var ftsHits int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_fts WHERE topic_fts MATCH 'mjpeg'").Scan(&ftsHits); err != nil {
		t.Fatal(err)
	}
	if ftsHits != 1 {
		t.Errorf("FTS hits for 'mjpeg' = %d, want 1", ftsHits)
	}

	// Reference row
	var refCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_reference").Scan(&refCount); err != nil {
		t.Fatal(err)
	}
	if refCount != 1 {
		t.Errorf("topic_reference count = %d, want 1", refCount)
	}
}

func TestIndexLayer_idempotentRebuild(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"mjpeg-performance.md": sampleTopic,
	})

	for i := 0; i < 3; i++ {
		if _, err := db.IndexLayer(layer); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM topic_index").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Errorf("iter %d: count = %d, want 1", i, count)
		}
	}
}

func TestIndexLayer_skipsBrokenFile(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"good.md":   sampleTopic,
		"broken.md": "no frontmatter at all\n",
	})

	result, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 {
		t.Errorf("Indexed = %d, want 1", result.Indexed)
	}
	if result.Failed != 1 {
		t.Errorf("Failed = %d, want 1", result.Failed)
	}
}

func TestIndexLayer_scopeMismatchRejected(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// File declares project:demo but we mount it as project:other
	layer := setupProjectLayer(t, "project:other", map[string]string{
		"mjpeg-performance.md": sampleTopic,
	})

	result, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failed (scope mismatch), got %d", result.Failed)
	}
}

func TestIndexLayer_emptyNotesDir(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := t.TempDir()
	layer := Layer{
		Scope:    "personal",
		RootPath: root,
		NotesDir: filepath.Join(root, "topics"), // intentionally not created
		Meta:     Meta{Scope: "personal"},
	}
	result, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 0 || result.Failed != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}
