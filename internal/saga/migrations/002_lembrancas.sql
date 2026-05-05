-- Saga v2 migration 002 — episodic layer (L2 in COGNITIVE_MODEL).
--
-- One row per act of bringing a memory to the conversation. The "memória"
-- (the topic note in topic_index) is durable; the "lembrança" is the event
-- of using it. Distinction matters: same memory can have many lembranças.
--
-- Known limitation: ON DELETE CASCADE means a `saga reindex` (which currently
-- DELETEs and re-INSERTs topic_index rows for a layer) loses lembrança
-- history for affected topics. Acceptable for Phase 1 (reindex is rare and
-- manual). Iter 4+ may refactor indexer to UPSERT-only, preserving history.

CREATE TABLE lembranca (
  id           TEXT PRIMARY KEY,                       -- ULID
  topic_id     TEXT NOT NULL,
  triggered_at INTEGER NOT NULL,                        -- unix ms
  kind         TEXT NOT NULL CHECK(kind IN ('hook','recall','topic_read','baseline')),
  query        TEXT,                                    -- query text; NULL for baseline
  cwd          TEXT,                                    -- working dir at trigger time
  was_used     INTEGER,                                 -- 0/1, NULL until feedback (Iter 4)
  outcome      TEXT,                                    -- helpful|irrelevant|wrong, NULL until feedback
  FOREIGN KEY (topic_id) REFERENCES topic_index(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX idx_lembranca_triggered     ON lembranca(triggered_at DESC);
CREATE INDEX idx_lembranca_topic         ON lembranca(topic_id);
CREATE INDEX idx_lembranca_kind          ON lembranca(kind);
CREATE INDEX idx_lembranca_topic_recency ON lembranca(topic_id, triggered_at DESC);
