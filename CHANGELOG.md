# Changelog

All notable changes to Saga are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and Saga adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each release is also published on the GitHub releases page with the
goreleaser-generated commit-level changelog and signed checksums.

## [Unreleased]

## [1.0.0-rc.5] — 2026-08-15

### Fixed
- **Critical data loss: moving a note between layers destroyed its usage
  history.** Filing a personal note into a project layer (same `id`, new
  `scope`) wiped every `lembranca` row for it. `pruneUnseenTopics` drops index
  rows for notes no longer on disk *in that layer*, and from inside one layer's
  pass a deleted note and a moved one are indistinguishable; `ON DELETE
  CASCADE` then took the history before the destination layer re-inserted the
  same id. Found by dogfooding: 8 notes moved, 76 rows lost, recovered only
  because a backup existed.

  Migration 005 rebuilds `lembranca` without the foreign key. The relationship
  was backwards: `topic_index` is a derived cache that `saga reindex` rebuilds
  from the `.md` files, while `lembranca` is an append-only record of what
  happened, reconstructible from nothing. `ON DELETE SET NULL` was rejected
  because `topic_id` is the only handle back to the note, and nulling it
  discards exactly what is needed to reconnect history when the note reappears
  under a new layer. This is the third member of the family after the two
  cascade bugs fixed in rc.3, and closes the design question left open in
  `002_lembrancas.sql`.
- **Empty `synonyms` and `triggers` were stored as `null`, not `[]`.**
  `json.Marshal` of a nil slice yields `"null"`, so a note with no entries and
  one with an empty list were stored differently, and queries of the obvious
  form (`WHERE triggers != '[]'`) read as correct while matching every note
  that simply had none. Existing rows normalise on the next reindex.

### Added
- **Always-on rule catalogue.** `policy` notes were invisible to the always-on
  path, which queried `profile` + `preference` only, so a rule reached the model
  only when a prompt happened to match it lexically. Rules the user had written
  were therefore broken in silence. The hook now emits `<saga-rules>`: an index
  of every policy in force for the working directory, by name and trigger
  phrase, with the bodies left to `topic_read`. Pointers rather than content —
  the bodies would cost roughly ten times as much on every prompt.
- **Activation triggers (spec 1.1).** A topic can declare `triggers` — patterns
  over host-supplied action identifiers — and `enforcement` (`advise` or
  `block`). Documented in §2.4 of the topic spec as backward-compatible v1.x
  additions. Deliberately host-neutral: an action identifier is an opaque
  string owned by the host, so the same mechanism serves a coding agent
  (`Bash(git commit*)`) and any other domain (`emr.write(patient/*)`). The
  engine matches patterns and never interprets what an action means.
- **`saga guard`**, a `PreToolUse` hook that supplies the rules governing the
  action about to run, or denies it when a matching rule declares
  `enforcement: block`. Deterministic where the catalogue depends on attention,
  and free when nothing matches. A rule delivered this way drops out of the
  catalogue, so the always-on cost falls as coverage improves instead of growing
  with the store. `saga setup-claude` now registers both hook events.
- **`saga vault`**, assembling every layer into one directory that opens as a
  single Obsidian vault: one graph across personal and every project, rather
  than a graph per repository. Symlinks, not copies, so the notes stay where
  they are and saga remains the only writer of record. Refuses to replace
  anything that is not already a symlink.
- **`saga rules`** lists the policies in force, with `--budget` to reproduce
  exactly what the hook injects.
- **`saga gc`** reports usage history whose topic is no longer indexed, and
  deletes only topic ids handed to it explicitly. Blind pruning is deliberately
  absent: a note filed in a layer that is inactive in the current directory is
  indistinguishable from a deleted one.

### Changed
- **The identity baseline now represents every note instead of the first few.**
  Nine notes totalling ~17.9k characters were concatenated and cut at a 400-token
  cap, so the first profile note consumed the whole budget and every
  `preference` note was dropped — stated preferences had never reached the
  model. Each note now receives a share of the budget, surplus from short notes
  is redistributed, and each is cut at a paragraph boundary. Measured on a real
  store: 9 of 9 notes present, in fewer characters than before.
- The rule catalogue and the baseline hold separate budgets. Sharing one is how
  policies became invisible in the first place.

## [1.0.0-rc.4] — 2026-07-24

### Fixed
- **Reindex regression introduced in rc.3: a note that kept its title but
  changed id vanished from the index.** The rc.3 reindex upserted by id and
  pruned stale rows *after*, so a re-id'd note collided with its own stale row
  on `UNIQUE(scope, title)`, failed to insert, and was then pruned — losing the
  topic (the `.md` on disk was untouched). `IndexLayer` now runs in three
  phases: parse all notes, **prune stale ids first**, then persist. The stale
  row is gone before the insert, so the title is free. Genuine duplicates (two
  files sharing scope+title) still surface as a per-file error, and one row
  survives. Regression tests cover both.

## [1.0.0-rc.3] — 2026-07-22

### Security
- Bumped `golang.org/x/text` 0.37.0 → 0.39.0 to resolve GO-2026-5970
  (infinite loop on invalid input), which `saga.Slugify` reaches through
  `transform.String`.

### Fixed
- **Critical data loss: `lembranca` history was wiped on upgrade and on
  reindex.** Two independent bugs destroyed the episodic usage history
  (39k+ rows in the field), both via `ON DELETE CASCADE` from
  `lembranca.topic_id`:
  - The migration runner rebuilt `topic_index` (migrations 003/004) inside a
    transaction with foreign keys enabled. `DROP TABLE topic_index` cascaded
    and emptied `lembranca`. `PRAGMA defer_foreign_keys` does not help — it
    defers constraint *checking*, not cascade *actions*. The runner now
    applies each migration on a pinned connection with `PRAGMA
    foreign_keys=OFF` (which cannot be toggled inside a transaction), then
    verifies integrity with `PRAGMA foreign_key_check`.
  - `saga reindex` wiped-and-rebuilt each layer (`DELETE FROM topic_index
    WHERE source_layer`), cascading away history on every run. `IndexLayer`
    now upserts topics by id and prunes only rows whose note file is gone, so
    surviving topics keep their history.
  - Regression tests seed a beta.3 DB, apply the rebuild migrations, reindex,
    and assert `lembranca` and `foreign_key_check` survive intact.

### Added
- CycloneDX SBOM (one per archive) and Cosign keyless signature of the
  `checksums.txt` manifest are now generated and published on every tagged
  release via the `release` workflow. Verification recipe documented in
  `.goreleaser.yaml`.
- Sequence diagram (prompt lifecycle) and storage flowchart in `README.md`
  to explain how recall, hook injection, and `topic_write` compose at
  request time.

### Changed
- `SECURITY.md` "What Saga protects" no longer claims supply-chain
  guarantees that the previous release artifacts did not actually produce.
  The claim returns automatically as truth from the next tagged release
  forward.

## [1.0.0-rc.1] — 2026-05-11

First release candidate. Sprint 0 closed: every spec-mandated surface that
gates Saga Topic Spec v1.0 Level-2 conformance is implemented.

### Added
- **`saga lint`** — spec v1.0 conformance validator. Eleven diagnostic
  categories (parse errors, missing fields, invalid type, invalid trait
  enums, scope mismatch, unknown operators, dangling relations,
  `@supersedes` / `@derived_from` cycles, slug ↔ title coherence,
  duplicate ids, missing recommended frontmatter). Flags: `--scope`,
  `--fix` (safe insertions only), `--format human|json`. Exit codes
  0 / 1 / 2. (#19)
- **`saga sync --dry-run`** — preview the push plan (pending changes
  and excluded confidential topics) without committing, pulling, or
  pushing.
- **`sensitivity: confidential` opt-out from sync** — topics with that
  frontmatter value are filtered out of `git add` via an exclude
  pathspec and never reach the remote. Surfaces a warning when a
  confidential file already exists in `origin/<branch>` (was pushed
  before the flip). (#22)
- **`saga show`** — display a topic plus its incoming and outgoing
  relations. (#17)
- **`saga conflicts`** — list `@conflicts_with` topic pairs in active
  layers, deduplicated regardless of which side declared the relation.
  (#16)
- **`saga capabilities`** — print the engine's capability declaration
  (spec version, conformance level, types implemented vs.
  accepted-opaque, operator support, retrieval features). Also exposed
  as MCP tool `saga_capabilities`. (#20)
- **Operator-aware recall** — `@supersedes` skips the superseded target
  by default; `@refines` adds a score boost to the refiner;
  `@conflicts_with` decorates both sides of the pair. (#15, #16, #17)
- **`topic_write` hygiene** — pre-write secret-pattern scanner (AWS,
  GitHub, OpenAI, Anthropic, SSH private keys, JWTs, DB connection
  strings, Stripe, Slack) plus Jaccard similarity warning at ≥ 0.6
  against existing topics in the target scope. (#13, #21)
- **Expanded type vocabulary** — fourteen spec types accepted by the
  engine (four implemented + ten accepted-opaque). Unknown types are
  rejected on write. (#18)
- **Topic relations** — six pure-metadata operators parse and persist
  (`@supersedes`, `@deprecated`, `@derived_from`, `@conflicts_with`,
  `@relates_to`, `@refines`). (#14)
- **`SECURITY.md`** — full threat model: trust boundaries, storage at
  rest, sync transport, secret handling, what `sensitivity: confidential`
  does and does not do, scope of vulnerability reports, disclosure
  timeline. Linked from `README.md`. (#23)
- **Personal layer seed default** is now `sensitivity_default: internal`.
  Confidential is the explicit per-topic opt-out, consistent with sync
  semantics.

### Changed
- README updated to reflect the Sprint 0 surface (relations, capabilities,
  hygiene). (#67)

### Documentation
- Saga Topic Spec v1.0 (draft) published under `docs/spec/`.

### Fixed
- Snapshot build no longer breaks on pre-tag PRs (SemVer-safe template).
- Hook output bounded to keep `<saga-context>` within Claude Code's
  injection budget.

## [1.0.0-beta.3] — 2026-05-06

### Fixed
- Version string in releases and `go install` builds.

## [1.0.0-beta.2] — 2026-05-06

### Added
- Public install instructions in README.

### Changed
- Internal release playbook moved out of the public-facing tree.

## [1.0.0-beta.1] — 2026-05-06

First public pre-release.

### Added
- Single-binary `saga` command with subcommands: `init`, `reindex`, `sync`,
  `lembrancas`, `doctor`, `mcp`, `hook`, `setup-claude`.
- Personal layer (`~/.saga/personal/`) and project layer (`<project>/.saga/`)
  with automatic resolution.
- SQLite + FTS5 + BM25 recall, regenerable from the markdown source of truth.
- `UserPromptSubmit` hook for Claude Code that injects `<saga-meta>`,
  `<saga-identity>`, and `<saga-context>` blocks into every prompt within
  a 2000-token ceiling.
- MCP tools: `recall`, `topic_read`, `topic_list`, `topic_write`,
  `lembranca_log`.
- Multi-machine sync of the personal layer via `saga sync` against the
  user's own private git remote.
- Apache-2.0 license, SECURITY.md, CONTRIBUTING.md, CODE_OF_CONDUCT.md.

[Unreleased]: https://github.com/mopanc/saga/compare/v1.0.0-rc.1...HEAD
[1.0.0-rc.1]: https://github.com/mopanc/saga/compare/v1.0.0-beta.3...v1.0.0-rc.1
[1.0.0-beta.3]: https://github.com/mopanc/saga/compare/v1.0.0-beta.2...v1.0.0-beta.3
[1.0.0-beta.2]: https://github.com/mopanc/saga/compare/v1.0.0-beta.1...v1.0.0-beta.2
[1.0.0-beta.1]: https://github.com/mopanc/saga/releases/tag/v1.0.0-beta.1
