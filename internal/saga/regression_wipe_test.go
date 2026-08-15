package saga

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// assertNoFKViolations fails the test if PRAGMA foreign_key_check reports any
// dangling reference.
func assertNoFKViolations(t *testing.T, db *DB) {
	t.Helper()
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if rows.Next() {
		t.Fatal("foreign_key_check reported violations")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// seedBeta3 builds a DB at the beta.3 schema (migrations 001 + 002 applied and
// recorded) with one topic and the given number of lembrança rows, then closes
// it so the caller can reopen through OpenDB (which applies 003 + 004).
func seedBeta3(t *testing.T, path string, lembrancas int) {
	t.Helper()
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	raw, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()

	if _, err := raw.Exec(`CREATE TABLE _migrations (
		version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"001_init", "002_lembrancas"} {
		script, err := fs.ReadFile(migrationsFS, "migrations/"+v+".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(string(script)); err != nil {
			t.Fatalf("apply %s: %v", v, err)
		}
		if _, err := raw.Exec("INSERT INTO _migrations(version, applied_at) VALUES (?, 0)", v); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := raw.Exec(`INSERT INTO topic_index
		(id, scope, type, title, file_path, file_hash, source_layer, created_at, updated_at)
		VALUES ('T1','personal','topic','Gold','/x.md','h','personal',0,0)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < lembrancas; i++ {
		if _, err := raw.Exec(
			`INSERT INTO lembranca (id, topic_id, triggered_at, kind) VALUES (?, 'T1', ?, 'recall')`,
			ulidLike(i), int64(i),
		); err != nil {
			t.Fatal(err)
		}
	}
}

func ulidLike(i int) string {
	return "L" + string(rune('A'+i%26)) + string(rune('0'+i/26%10))
}

// TestMigration_preservesLembrancaAcrossRebuild is the mandatory regression for
// the rc.2 data-loss bug: applying the table-rebuild migrations (003 + 004)
// must NOT cascade-delete the lembrança history.
func TestMigration_preservesLembrancaAcrossRebuild(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.db")

	const want = 39
	seedBeta3(t, path, want)

	db, err := OpenDB(path) // applies 003 + 004
	if err != nil {
		t.Fatalf("OpenDB (apply 003/004): %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("migration wiped lembrança: got %d, want %d", got, want)
	}
	assertNoFKViolations(t, db)
}

// TestIndexLayer_preservesLembrancaOnReindex is the mandatory regression for
// the reindex path: reindexing a layer whose notes are unchanged must keep the
// lembrança history of the surviving topics.
func TestIndexLayer_preservesLembrancaOnReindex(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"mjpeg-performance.md": sampleTopic,
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// id declared in sampleTopic's frontmatter.
	const topicID = "01HXY5KZQVJ8M3R7ABCDEFGHIJ"
	if err := db.LogLembrancas([]string{topicID}, LembrancaKindRecall, "q1", "/tmp"); err != nil {
		t.Fatalf("log lembranca: %v", err)
	}
	if err := db.LogLembrancas([]string{topicID}, LembrancaKindHook, "q2", "/tmp"); err != nil {
		t.Fatalf("log lembranca: %v", err)
	}

	var before int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before != 2 {
		t.Fatalf("setup: lembranca = %d, want 2", before)
	}

	// Reindex the unchanged layer — must not destroy history.
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	var after int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("reindex destroyed lembrança history: got %d, want %d", after, before)
	}
	assertNoFKViolations(t, db)
}

// noteWithID renders a minimal valid topic file with a given id and title.
func noteWithID(id, title string) string {
	return "---\nid: " + id + "\nscope: project:demo\ntype: topic\ntitle: " + title + "\n" +
		"created_at: 2026-04-12T10:30:00Z\nupdated_at: 2026-04-12T10:30:00Z\n---\n\nbody\n"
}

// TestIndexLayer_reindexSurvivesIdChangeSameTitle is the regression for the
// rc.3 reindex bug: a note that keeps its title but changes id (a reorg
// shuffle) must not collide with its own stale row and vanish. Pruning runs
// before persisting, so the stale id is removed first and the note re-indexes
// cleanly.
func TestIndexLayer_reindexSurvivesIdChangeSameTitle(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"a.md": noteWithID("01HXY5KZQVJ8M3R7ABCDEFGHIA", "Shared Title"),
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Reorg: same title, new id.
	if err := os.WriteFile(filepath.Join(layer.NotesDir, "a.md"),
		[]byte(noteWithID("01HXY5KZQVJ8M3R7ABCDEFGHIB", "Shared Title")), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if r.Failed != 0 {
		t.Errorf("reindex failed=%d, want 0; errors: %+v", r.Failed, r.Errors)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index WHERE source_layer='project:demo'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("note vanished after id-change reindex: count=%d, want 1", count)
	}
	assertNoFKViolations(t, db)
}

// TestIndexLayer_duplicateTitleStillErrors locks in that two distinct files
// sharing (scope, title) remain a per-file error — one survives, the other is
// reported — rather than silently overwriting or vanishing both.
func TestIndexLayer_duplicateTitleStillErrors(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"a.md": noteWithID("01HXY5KZQVJ8M3R7ABCDEFGHIA", "Dup Title"),
		"b.md": noteWithID("01HXY5KZQVJ8M3R7ABCDEFGHIB", "Dup Title"),
	})

	r, err := db.IndexLayer(layer)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if r.Failed != 1 {
		t.Errorf("duplicate title: failed=%d, want 1", r.Failed)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index WHERE title='Dup Title'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("duplicate title: %d rows survived, want exactly 1", count)
	}
	assertNoFKViolations(t, db)
}

// TestIndexLayer_prunesDeletedNotes documents that a note removed from disk is
// pruned from the index on the next reindex (and cannot leave FK violations).
func TestIndexLayer_prunesDeletedNotes(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"mjpeg-performance.md": sampleTopic,
		"socket-protocol.md":   sampleTopicTwo,
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("first index: %v", err)
	}

	if err := os.Remove(filepath.Join(layer.NotesDir, "socket-protocol.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("reindex after delete: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("deleted note not pruned: topic_index = %d, want 1", count)
	}
	assertNoFKViolations(t, db)
}

// noteInScope renders a minimal valid topic file for an arbitrary scope, so a
// test can move the same id between layers the way a real reorg does.
func noteInScope(id, scope, title string) string {
	return "---\nid: " + id + "\nscope: " + scope + "\ntype: topic\ntitle: " + title + "\n" +
		"created_at: 2026-04-12T10:30:00Z\nupdated_at: 2026-04-12T10:30:00Z\n---\n\nbody\n"
}

// TestIndexLayer_moveBetweenLayersPreservesLembranca is the regression for #95,
// found by dogfooding: filing a personal note into a project layer (same id,
// new scope) used to destroy its entire usage history. The source layer's
// prune deleted the index row before the destination layer re-inserted it, and
// ON DELETE CASCADE took the lembranças with it — 76 rows across 8 notes in the
// field. Migration 005 removed that FK; this test locks the behaviour in.
func TestIndexLayer_moveBetweenLayersPreservesLembranca(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const topicID = "01HXY5KZQVJ8M3R7ABCDEFGHIA"

	personal := setupProjectLayer(t, "personal", map[string]string{
		"npm-token-rotation.md": noteInScope(topicID, "personal", "NPM token rotation"),
	})
	if _, err := db.IndexLayer(personal); err != nil {
		t.Fatalf("index personal: %v", err)
	}

	const want = 25
	for i := 0; i < want; i++ {
		if err := db.LogLembrancas([]string{topicID}, LembrancaKindRecall, "q", "/tmp"); err != nil {
			t.Fatalf("log lembranca: %v", err)
		}
	}

	// The reorg: same id and title, now filed under the project layer.
	project := setupProjectLayer(t, "project:depguard", map[string]string{
		"npm-token-rotation.md": noteInScope(topicID, "project:depguard", "NPM token rotation"),
	})
	if err := os.Remove(filepath.Join(personal.NotesDir, "npm-token-rotation.md")); err != nil {
		t.Fatal(err)
	}

	// Reindex in the real order: source layer first, destination second.
	if _, err := db.IndexLayer(personal); err != nil {
		t.Fatalf("reindex personal after move: %v", err)
	}
	if _, err := db.IndexLayer(project); err != nil {
		t.Fatalf("index project: %v", err)
	}

	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM lembranca WHERE topic_id = ?", topicID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cross-layer move destroyed lembrança history: got %d, want %d", got, want)
	}

	var layer string
	if err := db.QueryRow("SELECT source_layer FROM topic_index WHERE id = ?", topicID).Scan(&layer); err != nil {
		t.Fatal(err)
	}
	if layer != "project:depguard" {
		t.Errorf("source_layer = %q, want project:depguard", layer)
	}
	assertNoFKViolations(t, db)
}

// TestPruneOrphanedLembrancas covers the other half of migration 005: history
// of a genuinely deleted note is retained (not cascaded away) and is reclaimable
// by `saga gc`, which only ever deletes ids it is handed explicitly.
func TestPruneOrphanedLembrancas(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const topicID = "01HXY5KZQVJ8M3R7ABCDEFGHIA"
	layer := setupProjectLayer(t, "project:demo", map[string]string{
		"a.md": noteInScope(topicID, "project:demo", "Doomed"),
	})
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("index: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := db.LogLembrancas([]string{topicID}, LembrancaKindRecall, "q", "/tmp"); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Remove(filepath.Join(layer.NotesDir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.IndexLayer(layer); err != nil {
		t.Fatalf("reindex after delete: %v", err)
	}

	// History outlives the note rather than being cascaded away.
	orphans, err := db.OrphanedLembrancas()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 1 || orphans[0].TopicID != topicID || orphans[0].Count != 3 {
		t.Fatalf("orphans = %+v, want 1 group of 3 for %s", orphans, topicID)
	}

	// gc never guesses: an id it was not given is left alone.
	if n, err := db.PruneOrphanedLembrancas([]string{"01HXY5KZQVJ8M3R7ABCDEFGHIZ"}); err != nil || n != 0 {
		t.Fatalf("prune of unrelated id: n=%d err=%v, want 0/nil", n, err)
	}

	n, err := db.PruneOrphanedLembrancas([]string{topicID})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("pruned %d, want 3", n)
	}
	assertNoFKViolations(t, db)
}
