# Changelog

All notable changes to Saga are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and Saga adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Each release is also published on the GitHub releases page with the
goreleaser-generated commit-level changelog and signed checksums.

## [Unreleased]

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
