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
