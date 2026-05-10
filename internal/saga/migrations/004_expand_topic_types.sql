-- Saga v2 migration 004 — expand topic type CHECK to spec v1.0 list.
--
-- The original 001_init.sql CHECK accepted only the 4 types implemented by
-- the early engine: profile, preference, policy, topic. Spec v1.0 §4
-- defines 14 types total; v1.0 engine implements behaviour for those 4 and
-- accepts the remaining 10 as opaque (parsed, indexed, fall-through to
-- topic-style retrieval). Reference: internal/saga/capabilities.go.
--
-- SQLite does not support ALTER COLUMN / DROP CONSTRAINT, so we rebuild the
-- table. PRAGMA defer_foreign_keys = 1 is required because topic_reference
-- and topic_relation declare FOREIGN KEY (topic_id|source_id) REFERENCES
-- topic_index(id); without the deferral, DROP TABLE topic_index would fail.

PRAGMA defer_foreign_keys = 1;

CREATE TABLE topic_index_new (
  id           TEXT PRIMARY KEY,
  scope        TEXT NOT NULL,
  type         TEXT NOT NULL CHECK(type IN (
    'profile','preference','policy','topic',
    'convention','fact',
    'workflow','runbook','skill',
    'incident','investigation','decision','observation','hypothesis'
  )),
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

INSERT INTO topic_index_new SELECT * FROM topic_index;
DROP TABLE topic_index;
ALTER TABLE topic_index_new RENAME TO topic_index;

-- The original indexes were attached to the old table name; CREATE INDEX
-- statements survive the rename only when the index lives in the same
-- schema. Recreating explicitly is the safe choice.
CREATE INDEX idx_topic_scope   ON topic_index(scope);
CREATE INDEX idx_topic_type    ON topic_index(type);
CREATE INDEX idx_topic_layer   ON topic_index(source_layer);
CREATE INDEX idx_topic_updated ON topic_index(updated_at DESC);
