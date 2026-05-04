-- Saga v2 — initial schema.
-- SQLite holds an index over markdown files; the .md is the source of truth.
-- Drop and `saga reindex` rebuilds from disk.

CREATE TABLE topic_index (
  id           TEXT PRIMARY KEY,
  scope        TEXT NOT NULL,
  type         TEXT NOT NULL CHECK(type IN ('profile','preference','policy','topic')),
  title        TEXT NOT NULL,
  synonyms     TEXT NOT NULL DEFAULT '[]',
  sensitivity  TEXT NOT NULL DEFAULT 'internal',
  confidence   TEXT NOT NULL DEFAULT 'proposed',
  file_path    TEXT NOT NULL,
  file_hash    TEXT NOT NULL,
  embedding    BLOB,
  source_layer TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE(scope, title)
) STRICT;

CREATE INDEX idx_topic_scope   ON topic_index(scope);
CREATE INDEX idx_topic_type    ON topic_index(type);
CREATE INDEX idx_topic_layer   ON topic_index(source_layer);
CREATE INDEX idx_topic_updated ON topic_index(updated_at DESC);

-- FTS5 over title, synonyms, body (fallback retrieval).
CREATE VIRTUAL TABLE topic_fts USING fts5(
  id UNINDEXED,
  scope UNINDEXED,
  title,
  synonyms,
  body,
  tokenize = 'unicode61 remove_diacritics 2'
);

-- References — for staleness checks against git blame.
CREATE TABLE topic_reference (
  topic_id    TEXT NOT NULL,
  path        TEXT NOT NULL,
  lines       TEXT,
  blame_hash  TEXT NOT NULL,
  is_stale    INTEGER NOT NULL DEFAULT 0,
  checked_at  INTEGER,
  PRIMARY KEY (topic_id, path),
  FOREIGN KEY (topic_id) REFERENCES topic_index(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX idx_reference_path ON topic_reference(path);
