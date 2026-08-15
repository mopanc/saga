-- Saga v2 migration 005 — lembrança outlives the topic leaving the index.
--
-- 002_lembrancas.sql left this open: "Whether usage history should outlive the
-- deletion of its note (ON DELETE SET NULL) remains an open design decision."
-- Field evidence closed it. See #95.
--
-- The FK's ON DELETE CASCADE made the episodic layer a dependent of the index.
-- That is the wrong relationship. topic_index is a derived cache — `saga
-- reindex` rebuilds it from the .md files on disk. lembranca is the opposite:
-- an append-only record of things that actually happened, reconstructible from
-- nothing. Letting the cache's bookkeeping delete the ledger inverted the value
-- ordering, and cost 76 lembranças across 8 notes in the field before this fix.
--
-- Concretely: pruneUnseenTopics deletes index rows for notes no longer on disk
-- *in that layer*. Moving a note from `personal` to `project:x` therefore
-- deleted its row while the source layer was reindexed, cascading its history
-- away, before the destination layer re-inserted the same id.
--
-- ON DELETE SET NULL was rejected: topic_id is the only handle back to the
-- note, so nulling it discards exactly the information needed to reconnect the
-- history when the note reappears under a new layer. Keeping the id and
-- dropping the constraint preserves that link across any move.
--
-- Consequence, intended: lembranças may now reference ids absent from
-- topic_index (a genuinely deleted note, or a note mid-move). Those are history,
-- not corruption, and PRAGMA foreign_key_check no longer reports them. `saga
-- gc` reclaims the ones whose note is really gone for good.
--
-- Table rebuild follows the 004 pattern: no per-file pragma. The migration
-- runner (applyMigration in db.go) pins a connection with PRAGMA
-- foreign_keys=OFF and verifies with foreign_key_check afterwards.

CREATE TABLE lembranca_new (
  id           TEXT PRIMARY KEY,                       -- ULID
  topic_id     TEXT NOT NULL,                          -- topic_index.id; intentionally NOT a foreign key
  triggered_at INTEGER NOT NULL,                        -- unix ms
  kind         TEXT NOT NULL CHECK(kind IN ('hook','recall','topic_read','baseline')),
  query        TEXT,                                    -- query text; NULL for baseline
  cwd          TEXT,                                    -- working dir at trigger time
  was_used     INTEGER,                                 -- 0/1, NULL until feedback (Iter 4)
  outcome      TEXT                                     -- helpful|irrelevant|wrong, NULL until feedback
) STRICT;

INSERT INTO lembranca_new (id, topic_id, triggered_at, kind, query, cwd, was_used, outcome)
SELECT id, topic_id, triggered_at, kind, query, cwd, was_used, outcome FROM lembranca;

DROP TABLE lembranca;

ALTER TABLE lembranca_new RENAME TO lembranca;

CREATE INDEX idx_lembranca_triggered     ON lembranca(triggered_at DESC);
CREATE INDEX idx_lembranca_topic         ON lembranca(topic_id);
CREATE INDEX idx_lembranca_kind          ON lembranca(kind);
CREATE INDEX idx_lembranca_topic_recency ON lembranca(topic_id, triggered_at DESC);
