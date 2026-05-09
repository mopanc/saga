<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 Jorge Morais and the Saga contributors

This document is licensed under the Apache License, Version 2.0.
You may obtain a copy of the License at: https://www.apache.org/licenses/LICENSE-2.0
-->

# Saga Topic Specification — v1.0 (draft)

> **Status:** Draft. Spec versioned independently from the reference engine.
> **Audience:** runtime authors, tool authors, and humans curating Saga topics.
> **Stability:** v1.0 frozen at first numbered release; v1.x adds; v2 may break.

## 0. Why a spec

Saga is a **cognitive substrate over markdown** — a semantic contract that lets humans and AI agents share durable, addressable, composable knowledge.

The product is the spec, not any particular engine. Markdown is the carrier; a versioned spec plus a canonical reference implementation is the moat. The model is ECMAScript / V8: an open spec that anyone can implement, and a reference engine that catches up to the spec over time.

This document defines the **on-disk contract**: what a topic is, how it relates to other topics, how runtimes negotiate features. It deliberately **specifies more than v1 engines implement**. The runtime catalog (§7) marks which operators are pure-metadata (any conformant runtime honours them) and which require runtime cognition (LLM inference, contradiction detection, etc.).

This spec does **not** define: storage layout, indexing strategy, retrieval ranking, MCP wire format, CLI surface. Those are engine concerns and may differ between implementations.

### Conformance language

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, **MAY** are used as defined in [RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) when in ALL CAPS.

### Spec versioning

This is **Saga Topic Spec v1.0**. A topic file declares the spec version it targets via frontmatter (`saga_spec: "1.0"`, optional — absent means "latest 1.x compatible"). Engines MUST refuse topics whose declared major version they do not implement.

---

## 1. The four primitives

Everything in Saga composes from exactly four primitives. If a future feature does not fit, it is complexity creep and MUST be pushed back.

### 1.1 Topic

A unit of knowledge. Concretely, a UTF-8 markdown file with structured frontmatter. Self-contained, addressable by stable id and by slug, content-hashable.

A topic MUST have:

- A **stable id** — opaque string, unique within its layer. Engines SHOULD use ULID or UUIDv7. Never reused.
- A **slug** — filesystem-safe, human-readable handle (e.g. `saga-topic-spec`). MAY change; resolution by id is canonical.
- A **type** (§4) — declares the topic's role.
- A **title** — short human label.
- A **body** — markdown prose; the substantive content.

Topics are immutable in spirit (history is preserved via the layer's git history) but mutable in practice (the file on disk represents the latest revision).

### 1.2 Layer

A scoped, isolated boundary that contains topics. Has its own owner, sync remote, and sensitivity defaults.

A layer MUST:

- Have a `meta.yml` declaring its `scope` and an optional `name`.
- Be addressable as a directory tree the engine can list, read, and write.
- Be independently syncable (typically a git repo).

Standard scope values (engines MAY define more):

| Scope | Intended meaning |
|---|---|
| `personal` | One human, follows them across machines |
| `project` | One codebase, lives with the repo |
| `dept` | One department / squad, larger blast radius than project |
| `org` | One organisation, broadest team-internal layer |

Layers are independent. A topic in `personal` MUST NOT silently shadow a topic in `project`; engines MUST surface the conflict to the user or rank deterministically by an explicit precedence rule.

### 1.3 Relation

A typed, directed link between two topics. Encoded inline in the source topic via operators (§7), never as a side-table.

```yaml
# in foo.md frontmatter
relations:
  - { op: "@supersedes", target: "old-foo" }
  - { op: "@derived_from", target: "investigation-2026-04-12" }
```

Relations are **first-class data**, not commentary. Retrieval, lint, and synthesis all walk relations.

### 1.4 Retrieval

A query plus a ranking plus an injection rule. Deterministically composable.

The spec does **not** mandate a ranking algorithm. Engines MAY use BM25, vector similarity, recency weighting, valence, or any combination. The spec mandates only:

- Retrieval MUST honour layer scope (no cross-scope leakage unless explicitly configured).
- Retrieval MUST honour `confidence` and `lifecycle` traits (§5) when ranking.
- Retrieval MUST be **bounded** at the injection point (callers cannot trigger unbounded context blow-up).

---

## 2. File format

A topic is a UTF-8 markdown file consisting of:

```markdown
---
<YAML 1.2 frontmatter>
---

<markdown body>
```

### 2.1 Frontmatter — required fields

| Field | Type | Notes |
|---|---|---|
| `id` | string | Stable id, see §1.1 |
| `scope` | string | The layer's scope; MUST match the containing layer's `meta.yml` |
| `type` | string | One of the registered types (§4) |
| `title` | string | Human-readable, ≤ 200 chars |

### 2.2 Frontmatter — recommended fields

| Field | Type | Notes |
|---|---|---|
| `saga_spec` | string | Spec version this topic targets, e.g. `"1.0"` |
| `synonyms` | string[] | Alternate handles for retrieval and disambiguation |
| `created_at` | RFC 3339 timestamp | First write time |
| `updated_at` | RFC 3339 timestamp | Last write time |
| `relations` | object[] | See §1.3 |
| `confidence` | enum | See §5.1 |
| `lifecycle` | enum | See §5.2 |
| `provenance` | enum | See §5.3 |
| `memory_family` | enum | See §5.4 (auto-inferred from `type` if omitted) |
| `operator_surface` | enum | See §5.5 |
| `sensitivity` | enum | `public`, `internal`, `confidential` (default `internal`) |
| `tags` | string[] | Free-form labels |
| `requires` | object | Capability requirements (§8) |

### 2.3 Body

The body is markdown. It SHOULD be self-contained at the topic-grain (≈ 500 words). Engines MAY enforce a per-topic byte cap; the reference engine v1.0 caps at 8000 bytes for `topic_write`.

Content best practices (non-normative):

- Lead with the load-bearing fact or decision.
- Use prose, not bullet lists, for things that need to survive recall ranking.
- Date irreversible decisions inline.
- Cite related topics by slug, not by file path.

---

## 3. The three axes of memory

Topics map to one of three memory families. Lifecycle, decay, and retrieval ranking differ by family. Cognitive science alignment is the inspiration; the spec enforces only the engineering consequence.

| Axis | Engineering consequence | Default decay |
|---|---|---|
| **Declarative** — facts, preferences, identity, conventions | Stable; ranked by recency-of-validation | Low |
| **Procedural** — how-to, runbooks, workflows | Validated by execution; stale procedures degrade fast under change | Validated by use |
| **Episodic** — what happened: incidents, decisions, investigations, observations | High volume; aggressive decay; consolidatable into declarative | High |

A topic MUST belong to exactly one family. The family is auto-inferred from `type` (§4) but MAY be set explicitly in frontmatter to override.

Engines MAY apply different ranking weights per family. Engines MAY provide consolidation operators that promote N episodic topics into one declarative topic. The spec does not mandate a decay function — only that declared decay characteristics in frontmatter are advisory inputs to ranking.

---

## 4. Topic types

The type field declares functional role. The spec defines a vocabulary; engines MAY register additional types.

### 4.1 Declarative types

| Type | Purpose | Example |
|---|---|---|
| `profile` | Identity facts about an actor (human, project, system) | "Jorge — Tech Lead at Balanças Marques" |
| `preference` | Soft preferences: style, framing, tone | "Use PT-PT, not PT-BR" |
| `policy` | Hard rules: must-do, must-not | "Never use work email in personal repos" |
| `convention` | Project- or team-level shared conventions | "Conventional commits required" |
| `fact` | Standalone durable fact | "Production DB is Postgres 15.4" |
| `topic` | General-purpose semantic note (catch-all) | "How our auth flow works" |

### 4.2 Procedural types

| Type | Purpose | Example |
|---|---|---|
| `workflow` | A named ordered sequence of steps | "Cut a release" |
| `runbook` | Operational procedure for a known event | "Recover from index corruption" |
| `skill` | Reusable capability description | "How I approach bug triage" |

### 4.3 Episodic types

| Type | Purpose | Example |
|---|---|---|
| `incident` | A specific failure event with timeline | "Outage 2026-04-12" |
| `investigation` | A bounded inquiry: question, evidence, conclusion | "Why does the index regenerate slowly?" |
| `decision` | An ADR-shaped record: context, options, choice, why | "Adopt v8/ECMAScript model for spec vs engine" |
| `observation` | A noted fact-in-context, lower commitment than `fact` | "Recall p99 spiked after the 2026-04-30 reindex" |
| `hypothesis` | A claim under test, with success criteria | "Vector embeddings will improve recall@5 by 20%" |

### 4.4 Type evolution

- A topic's type MAY change via a typed operator (§7); engines SHOULD record the prior type in `relations` as `@derived_from` for audit.
- Engines MAY reject types they do not recognise (strict mode) or fall back to `topic` (lenient mode). Lenient mode MUST log the unknown type.

### 4.5 Reference engine v1.0 — supported types

| Status | Types |
|---|---|
| Implemented | `profile`, `preference`, `policy`, `topic` |
| Specced, not yet enforced by engine | all others above — accepted as opaque on write, treated as `topic` for retrieval |

---

## 5. The five orthogonal traits

Vocabulary stays small by replacing taxonomies of types with a few orthogonal traits. Five traits with three values each gives 243 distinct shapes — enough to express what 30 redundant types would.

### 5.1 `confidence`

| Value | Meaning |
|---|---|
| `canonical` | Validated; treat as authoritative until explicitly retired |
| `tentative` | Working assumption; subject to revision |
| `proposed` | Under discussion; not yet decided |

Default: `tentative`.

### 5.2 `lifecycle`

| Value | Meaning |
|---|---|
| `durable` | Long-lived, low decay |
| `volatile` | Time-bounded; stale fast |
| `archived` | No longer active; retained for history; SHOULD NOT be injected by default |

Default: `durable` for declarative; `volatile` for episodic; engine choice for procedural.

### 5.3 `provenance`

| Value | Meaning |
|---|---|
| `human_generated` | Authored by a human |
| `agent_generated` | Authored by an AI agent |
| `derived` | Synthesised from other topics by an engine operator |

Default: `agent_generated` if written via MCP `topic_write`; otherwise unset.

### 5.4 `memory_family`

| Value | Mapping |
|---|---|
| `declarative` | See §3 |
| `procedural` | See §3 |
| `episodic` | See §3 |

Default: inferred from `type` per §4.

### 5.5 `operator_surface`

| Value | Meaning |
|---|---|
| `inert` | Plain content; engines never execute anything from this topic |
| `executable` | Topic contains operators that an engine MAY run (e.g. `@synthesize` instructions) |

Default: `inert`. A topic MUST NOT execute anything by default; `executable` MUST be opt-in and the engine MUST require user confirmation before running operator-driven mutations on a layer.

---

## 6. Relations

Relations connect topics. They are typed, directed, and metadata-only — a relation does not by itself execute anything.

### 6.1 Encoding

```yaml
relations:
  - op: "@supersedes"
    target: "old-auth-policy"
  - op: "@derived_from"
    target: "investigation-2026-03-12"
    notes: "promoted to policy after three repeated incidents"
```

A relation MUST have `op` and `target`. `target` MAY be a slug (resolved within the topic's layer first, then ascending scopes) or a fully qualified `scope:slug`. Engines MUST refuse to silently ignore a dangling target — at minimum, surface it via lint.

### 6.2 The pure-metadata relation operators

These are spec-mandatory. Any conformant engine MUST parse and honour them in retrieval and lint:

| Operator | Semantics |
|---|---|
| `@supersedes` | Source replaces target. Retrieval MUST prefer the superseder. Target SHOULD be lifecycle-archived. |
| `@deprecated` | Source declares itself stale; target (optional) is the replacement. Retrieval MUST de-rank deprecated topics. |
| `@derived_from` | Source was generated from target(s). For audit and provenance. |
| `@conflicts_with` | Source contradicts target. Engines MUST surface the contradiction; ranking is engine choice. |
| `@relates_to` | Weak association. Hint to retrieval; no normative effect. |
| `@refines` | Source is a more specific instance of target. Useful when promoting an observation to a fact. |

### 6.3 Cycles

Cycles are illegal for `@supersedes` and `@derived_from`. Engines MUST detect and refuse them on write. Cycles are permitted for `@relates_to` and `@conflicts_with`.

---

## 7. Operators

Operators are spec-defined verbs. Two castes:

### 7.1 Pure-metadata operators

Zero runtime cost. Parser-only. Any engine that can read YAML can honour them. Listed in §6.2 above.

A topic that uses only pure-metadata operators MUST have `operator_surface: inert`.

### 7.2 Runtime-required operators

Require active cognition (LLM inference, contradiction detection, semantic equivalence). Engines that lack the required capability MUST refuse to execute these operators, but MUST still parse them and surface the gap clearly.

| Operator | Semantics | Required capability |
|---|---|---|
| `@synthesize` | Produce a new topic by combining N source topics | `llm_inference` |
| `@summarize` | Produce a shorter summary topic from a longer source | `llm_inference` |
| `@reconcile` | Resolve conflicts among N topics into a canonical resolution | `llm_inference`, `contradiction_detection` |
| `@promote` | Convert episodic observations into a declarative fact when threshold met | `pattern_recognition` |
| `@retire` | Move a topic to `lifecycle: archived` because criteria met | `lifecycle_inference` |

A topic that triggers any runtime-required operator MUST set `operator_surface: executable`.

### 7.3 Operator declaration in frontmatter

```yaml
operator_surface: executable
operators:
  - op: "@synthesize"
    sources: ["incident-2026-04-12", "incident-2026-04-19", "incident-2026-04-30"]
    target_type: "decision"
    output_slug: "auth-incidents-q2-decision"
```

Engines MUST require user confirmation (or explicit policy) before executing any operator block.

---

## 8. Capability negotiation

Topics declare what they need. Runtimes declare what they offer. Mismatches degrade gracefully.

### 8.1 Topic-side declaration

```yaml
requires:
  spec: "1.0"
  operators: [supersedes, conflicts_with, synthesize]
  cognition: [llm_inference]
```

An engine that does not offer a required capability MUST:

1. Index the topic (parse + store).
2. Mark the topic as **partially supported** in any UI/CLI surface.
3. Refuse to execute any unsupported operator.
4. Permit retrieval to return it (with the partial-support flag).

### 8.2 Engine-side declaration

Engines MUST publish their capability set somewhere discoverable (CLI command, MCP server-info response, etc.). The reference engine v1.0 publishes:

```yaml
spec_supported: ["1.0"]
operators: [supersedes, deprecated, derived_from, conflicts_with, relates_to, refines]
cognition: []                        # no runtime-required operators yet
types_implemented: [profile, preference, policy, topic]
types_specced_only: [convention, fact, workflow, runbook, skill, incident,
                     investigation, decision, observation, hypothesis]
```

### 8.3 Layered runtimes

A layered ecosystem is explicitly supported: a VS Code extension may implement only pure-metadata lint; a saga binary implements parsing + retrieval + sync; a future hosted engine adds cognition. All three are conformant **for the subset they declare**.

---

## 9. Topic identity and resolution

### 9.1 Identity

The `id` field is the canonical identity. Slugs MAY change; ids MUST NOT.

When a slug changes, the previous slug SHOULD remain in `synonyms`. Engines SHOULD index synonyms for resolution.

### 9.2 Resolution order

When a tool refers to a topic by handle:

1. Resolve as id within the active layer.
2. Resolve as slug within the active layer.
3. Resolve as id within ascending scopes (project → personal → dept → org).
4. Resolve as slug within ascending scopes.
5. Resolve via `synonyms`.
6. If still ambiguous, fail with a disambiguation prompt.

### 9.3 Cross-layer references

A relation MAY target a topic in a different scope using `scope:slug` syntax (e.g. `personal:saga-topic-spec`). Engines MUST refuse cross-layer writes (a `personal` topic cannot mutate a `project` topic via operator).

---

## 10. Conformance levels

An engine claims one or more of these levels.

### Level 0 — Reader

- MUST parse all required frontmatter fields.
- MUST tolerate unknown fields without erroring.
- MAY ignore relations and operators.

### Level 1 — Pure-metadata engine

- All Level 0 requirements.
- MUST parse and surface all relations from §6.2.
- MUST honour `@supersedes` and `@deprecated` in any ranking it performs.
- MUST refuse cycles per §6.3.

### Level 2 — Reference engine

- All Level 1 requirements.
- MUST implement retrieval honouring traits (§5).
- MUST implement lint covering: dangling relations, cycle detection, type validity, sensitivity defaults.
- MUST expose capability declaration per §8.2.

### Level 3 — Cognitive engine

- All Level 2 requirements.
- Implements one or more runtime-required operators from §7.2.
- MUST require user confirmation (or explicit policy) before mutations.

The reference engine targets Level 2 in v1.0; Level 3 capabilities arrive incrementally and are gated behind explicit configuration.

---

## 11. Reserved namespaces

Frontmatter keys with these prefixes are reserved by the spec:

- `saga_*` — spec-internal metadata (`saga_spec`, etc.)
- `_*` — engine-internal scratch state (engines MAY use; tools SHOULD ignore)

All other keys are user-defined and MUST be preserved verbatim by engines on round-trip read/write.

---

## 12. What is intentionally **not** in v1.0

These were considered and excluded. Each MAY return in a future version with explicit evidence of need.

| Excluded | Why |
|---|---|
| Decay automation (engine-driven `lifecycle` transitions) | Manual transitions only in v1.0; automation requires evidence of false-positive cost |
| Schema templates (situation-bound topic shapes) | No repeated pattern observed yet |
| Vector-only retrieval as spec mandate | Engine choice; spec does not mandate ranking |
| Per-topic ACLs beyond `sensitivity` | Layer-level ACL is the unit; per-topic ACL is creep |
| Built-in CRDT for collaborative edit | Git is the merge primitive; CRDT is engine option |
| Automatic translation of body content | Out of scope; carrier remains markdown |

---

## 13. Glossary

| Term | Definition |
|---|---|
| **Topic** | A markdown file with structured frontmatter; the unit of memory |
| **Layer** | A scoped, syncable container of topics |
| **Relation** | A typed, directed link between two topics |
| **Retrieval** | The pipeline that selects topics for injection given a query |
| **Family** | One of declarative / procedural / episodic |
| **Trait** | One of the five orthogonal axes in §5 |
| **Operator** | A spec-defined verb in pure-metadata or runtime-required form |
| **Capability** | An engine-side ability that operators MAY require |
| **Conformance** | An engine's claimed level (§10) |
| **Engine** | A program that reads, writes, retrieves, or operates on topics |
| **Carrier** | The on-disk encoding (markdown + YAML frontmatter) |
| **Substrate** | The semantic contract (this spec) |

---

## 14. Open questions (tracked, not blocking)

- Should `requires.cognition` be a closed enum or free-form? Leaning closed for v1.x.
- Should cross-engine signatures (provenance attestation) be in the spec? Likely v2.
- Should retrieval ranking become normative? Currently engine choice; risks fragmentation.
- Per-relation timestamps? Currently not normative; useful for audit.
- Operator output addressing (where does `@synthesize` write?). Currently engine choice.

---

## Appendix A — Minimal conformant topic

```markdown
---
id: 01KR775BATPHG6DF4EMZR83NMC
saga_spec: "1.0"
scope: personal
type: topic
title: Saga Topic Spec
---

The on-disk contract for Saga.
```

## Appendix B — Fully decorated topic

```markdown
---
id: 01KR91M6N2GQ7P3X4D8AY0BCZK
saga_spec: "1.0"
scope: project
type: decision
title: Adopt ECMAScript / V8 model for Saga
synonyms: [saga ADR, spec vs engine]
created_at: 2026-05-09T14:00:00Z
updated_at: 2026-05-09T18:30:00Z
confidence: canonical
lifecycle: durable
provenance: human_generated
memory_family: episodic
operator_surface: inert
sensitivity: internal
tags: [architecture, governance]
relations:
  - op: "@supersedes"
    target: "saga-vague-vision-2026-04"
  - op: "@derived_from"
    target: "investigation-saga-positioning-2026-05"
requires:
  spec: "1.0"
  operators: [supersedes, derived_from]
  cognition: []
---

Decision: Saga is positioned as a cognitive substrate over markdown. Spec
versioned independently from the canonical reference engine, modelled on
ECMAScript / V8.

(...rest of body...)
```
