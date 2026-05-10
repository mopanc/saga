package saga

import (
	"testing"
)

func TestOpenDB_inMemoryAppliesMigrations(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least 1 applied migration, got 0")
	}

	// Verify topic_index exists and is empty.
	var topicCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_index").Scan(&topicCount); err != nil {
		t.Fatalf("query topic_index: %v", err)
	}
	if topicCount != 0 {
		t.Errorf("topic_index count = %d, want 0", topicCount)
	}

	// Verify FTS5 virtual table exists.
	if _, err := db.Exec("INSERT INTO topic_fts (id, scope, title, synonyms, body) VALUES ('x', 'personal', 'test', '[]', 'body')"); err != nil {
		t.Fatalf("insert into topic_fts: %v", err)
	}
}

func TestMigration004_acceptsExpandedTypes(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert a row for each spec type — the CHECK constraint must accept them all.
	for i, typ := range SpecTypesAll() {
		id := "01HXY5KZQVJ8M3R7TESTSPEC0" + string(rune('A'+i))
		_, err := db.Exec(`
			INSERT INTO topic_index
			  (id, scope, type, title, file_path, file_hash, source_layer, created_at, updated_at)
			VALUES (?, 'personal', ?, ?, '/tmp/x.md', 'hash', 'personal', 0, 0)
		`, id, typ, "title-"+typ)
		if err != nil {
			t.Errorf("insert type %q: %v", typ, err)
		}
	}

	// And rejects a non-spec type.
	if _, err := db.Exec(`
		INSERT INTO topic_index
		  (id, scope, type, title, file_path, file_hash, source_layer, created_at, updated_at)
		VALUES ('rejected', 'personal', 'not_in_spec', 'x', '/tmp/y.md', 'h', 'personal', 0, 0)
	`); err == nil {
		t.Error("expected CHECK constraint to reject non-spec type")
	}
}

func TestMigration004_preservesCascadeOnRelations(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed a topic, a relation, and a reference.
	if _, err := db.Exec(`
		INSERT INTO topic_index (id, scope, type, title, file_path, file_hash, source_layer, created_at, updated_at)
		VALUES ('A', 'personal', 'topic', 'a', '/x.md', 'h', 'personal', 0, 0),
		       ('B', 'personal', 'topic', 'b', '/y.md', 'h', 'personal', 0, 0);
		INSERT INTO topic_relation (source_id, op, target_id) VALUES ('A', '@supersedes', 'B');
		INSERT INTO topic_reference (topic_id, path, blame_hash, is_stale) VALUES ('A', '/x.go', 'h', 0);
	`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec("DELETE FROM topic_index WHERE id = 'A'"); err != nil {
		t.Fatal(err)
	}

	// Cascade should have removed relations and references for A.
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_relation WHERE source_id = 'A'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("topic_relation cascade broken: %d rows remain after migration 004 rebuild", n)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM topic_reference WHERE topic_id = 'A'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("topic_reference cascade broken: %d rows remain", n)
	}
}

func TestOpenDB_idempotent(t *testing.T) {
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("first OpenDB: %v", err)
	}
	if err := db.applyMigrations(); err != nil {
		t.Fatalf("re-apply migrations: %v", err)
	}
	db.Close()
}
