-- Saga Phase 1 — initial schema.
-- Single table + FTS5 virtual table + sync triggers.
-- Embedding column is reserved (NULL in Phase 1); sqlite-vec lands in Phase 1.5
-- without disruptive migration.

CREATE TABLE memory (
  id          TEXT PRIMARY KEY,
  text        TEXT NOT NULL,
  tags        TEXT NOT NULL DEFAULT '[]',
  embedding   BLOB,
  source      TEXT,
  session_id  TEXT,
  created_at  INTEGER NOT NULL
) STRICT;

CREATE INDEX idx_memory_created_at ON memory(created_at DESC);

-- FTS5 virtual table for keyword retrieval.
-- `id UNINDEXED` keeps the value queryable without full-text indexing it.
CREATE VIRTUAL TABLE memory_fts USING fts5(
  id UNINDEXED,
  text,
  tags,
  tokenize = 'unicode61 remove_diacritics 2'
);

-- Triggers keep memory_fts in sync with memory.
CREATE TRIGGER memory_after_insert
AFTER INSERT ON memory BEGIN
  INSERT INTO memory_fts (id, text, tags)
  VALUES (NEW.id, NEW.text, NEW.tags);
END;

CREATE TRIGGER memory_after_delete
AFTER DELETE ON memory BEGIN
  DELETE FROM memory_fts WHERE id = OLD.id;
END;

CREATE TRIGGER memory_after_update
AFTER UPDATE OF text, tags ON memory BEGIN
  UPDATE memory_fts SET text = NEW.text, tags = NEW.tags WHERE id = NEW.id;
END;
