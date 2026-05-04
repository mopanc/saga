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
