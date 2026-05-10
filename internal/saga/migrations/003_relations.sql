-- Saga v2 migration 003 — typed relations between topics.
--
-- A relation is a directed link from a source topic to a target topic, tagged
-- by an operator (@supersedes, @derived_from, @deprecated, @relates_to,
-- @conflicts_with, @refines). See spec §1.3 and §6.
--
-- target_id has NO foreign key on purpose: dangling references are surfaced
-- by `saga lint`, not blocked at write time. This keeps `topic_write` order-
-- independent and aligns with the spec's lenient-mode wording.
--
-- Cycles for @supersedes and @derived_from are MUST-detect per spec §6.3.
-- Detection lives in lint and (later) write-path validation; this schema
-- does not enforce it at the SQL layer.

CREATE TABLE topic_relation (
  source_id TEXT NOT NULL,
  op        TEXT NOT NULL,
  target_id TEXT NOT NULL,
  note      TEXT,
  PRIMARY KEY (source_id, op, target_id),
  FOREIGN KEY (source_id) REFERENCES topic_index(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX idx_topic_relation_target ON topic_relation(target_id);
CREATE INDEX idx_topic_relation_op     ON topic_relation(op);
