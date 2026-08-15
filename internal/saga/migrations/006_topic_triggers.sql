-- Saga v2 migration 006 — activation triggers on topics (spec v1.1).
--
-- A rule the user wrote is only useful if it is present at the moment it
-- applies. The always-on rule catalogue (see BuildRuleCatalog) makes every rule
-- discoverable, but discovery still depends on the agent noticing an entry is
-- relevant — the same class of failure it set out to fix, only cheaper.
--
-- `triggers` lets a topic declare the actions it governs, so the engine can
-- inject it deterministically when such an action is about to happen, rather
-- than hoping attention lands on it.
--
-- Deliberately host-neutral, per the spec generality principle: a trigger is a
-- pattern over an *action identifier*, and the namespace of action identifiers
-- belongs to the host. A coding agent supplies `Bash(git commit*)`; a clinical
-- host would supply `emr.write(*)`. The engine matches patterns; it does not
-- know what an action means.
--
-- Stored as a JSON array to mirror `synonyms`. NULL and '[]' both mean "no
-- triggers"; such topics remain reachable via the catalogue and recall.
--
-- ALTER TABLE ADD COLUMN is safe here: no table rebuild, so no cascade risk
-- (cf. 005). The runner still pins the connection and verifies with
-- foreign_key_check.

ALTER TABLE topic_index ADD COLUMN triggers TEXT NOT NULL DEFAULT '[]';

ALTER TABLE topic_index ADD COLUMN enforcement TEXT NOT NULL DEFAULT 'advise';
