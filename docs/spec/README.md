<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 Jorge Morais and the Saga contributors
-->

# Saga Specification

This directory holds the **Saga Topic Specification** — the on-disk contract that defines what a Saga topic is, how topics relate, and how runtimes negotiate features.

## Spec vs engine

Saga distinguishes two artefacts on purpose:

| Artefact | What it is | Lives in |
|---|---|---|
| **Specification** | The semantic contract: file format, primitives, relations, operators, conformance levels | `docs/spec/` |
| **Reference engine** | The canonical implementation: indexer, retrieval, MCP server, CLI | `cmd/saga/`, `internal/` |

The specification is the product. The engine is the canonical reference. Other engines are welcome — a VS Code lint extension, a server-side cognitive runtime, an editor plugin — and they conform by implementing one of the **conformance levels** defined in the spec.

The reference model is ECMAScript / V8: an open, versioned spec; an authoritative implementation; many compatible runtimes.

## Versioning policy

- The spec is versioned independently from the engine.
- A topic file MAY declare its target spec version with `saga_spec: "1.0"` in frontmatter.
- v1.x adds backward-compatible fields and operators.
- v2 may break compatibility; migration guidance will accompany any major bump.
- Engines declare which spec versions they support via their capability declaration.

## License

The specification is licensed under [Apache License, Version 2.0](../../LICENSE), the same license as the reference engine. Anyone is free to implement the spec, fork it, build on it, or commercialise an engine that conforms — provided the license terms are honoured.

## Contributing to the spec

Spec changes follow a heavier process than engine changes:

1. **Open an issue** describing the gap or proposed addition. Link to concrete topics or use cases that motivate it.
2. **Pre-mortem the change.** What does it break? What does it foreclose? What would the engine cost be?
3. **Submit a PR** updating both the spec doc and the relevant engine surface (or explicitly leave the engine catching up).
4. **Mark the change as additive (v1.x) or breaking (v2)** in the PR description.

The spec is intentionally **conservative**. It is far easier to add a field later than to remove a wrong one. When in doubt, leave it out.

## Current documents

| Document | Status | Purpose |
|---|---|---|
| [`saga-topic-v1.md`](saga-topic-v1.md) | Draft | The on-disk contract for topics, layers, relations, retrieval |

## Anti-creep filter

Every proposed addition is checked against three questions before merging:

1. **Does it fit the four primitives?** (Topic, Layer, Relation, Retrieval — §1 of v1.) If not, push back.
2. **Is it pure-metadata or runtime-required?** Pure-metadata additions are cheap; runtime-required additions cost cognition and MUST justify the cost.
3. **Does it create an enforced behaviour without an opt-out?** If yes, redesign — the spec describes the substrate, not the policy.

If a proposal fails any of the three, it is creep until proven otherwise.
