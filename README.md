# Saga — persistent memory for AI coding agents

[![CI](https://github.com/mopanc/saga/actions/workflows/ci.yml/badge.svg)](https://github.com/mopanc/saga/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/mopanc/saga?include_prereleases&sort=semver)](https://github.com/mopanc/saga/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/mopanc/saga.svg)](https://pkg.go.dev/github.com/mopanc/saga)
[![Go Report Card](https://goreportcard.com/badge/github.com/mopanc/saga)](https://goreportcard.com/report/github.com/mopanc/saga)
[![License: Apache-2.0](https://img.shields.io/github/license/mopanc/saga)](LICENSE)
[![CodeQL](https://github.com/mopanc/saga/actions/workflows/codeql.yml/badge.svg)](https://github.com/mopanc/saga/actions/workflows/codeql.yml)

> **Stop re-explaining your codebase to AI every session.** Saga is a topic-grained, layered memory layer for AI coding agents. It lets Claude Code, Cursor, Windsurf, Antigravity and any other [Model Context Protocol](https://modelcontextprotocol.io) (MCP) client remember what they learned across sessions — your codebase, your decisions, your preferences — so the next conversation starts where the last one ended.

Local-first. Single static binary. Markdown notes in git, indexed by SQLite. Cross-IDE, cross-machine.

## Why Saga

AI coding agents have no memory between sessions. Every conversation starts blank — you re-explain your stack, your conventions, the bug you fixed last week. For solo developers and small teams working on real codebases, this re-explanation costs hours every week.

Saga gives the agent a place to **write durable notes** after an investigation, and a fast retrieval path to **read them back** on the next session. The unit of memory is a curated *topic note* (~500 words, self-contained markdown), not a chunked sentence corpus — because investigation work needs coherent context, not scattered phrases.

Think of it as **git for cognition**: notes are versioned by markdown, related by typed operators (`@supersedes`, `@refines`, `@conflicts_with`), surfaced by ranking that respects what the AI has actually used. The retrieval layer doesn't just *search* relevant memories — it tracks which ones are still canonical, which evolved, and which are superseded.

## Highlights

- **Topic-grained, not chunk-grained.** ~500-word self-contained notes beat sentence-level chunking for code-investigation workloads.
- **Typed relations between memories.** Six pure-metadata operators — `@supersedes`, `@refines`, `@deprecated`, `@derived_from`, `@conflicts_with`, `@relates_to` — let the corpus evolve over time without becoming a dumpster.
- **Layered scopes.** `personal | project | dept | org` — each layer has an independent owner, sync remote, and sensitivity. Personal travels with you; project travels with the repo.
- **Cross-IDE via MCP.** Six tools (`recall`, `topic_read`, `topic_list`, `topic_write`, `lembranca_log`, `saga_capabilities`) exposed over JSON-RPC 2.0 stdio to any MCP client.
- **Auto-injection on Claude Code.** A `UserPromptSubmit` hook surfaces the relevant topic notes on every prompt so the agent never forgets to look.
- **Multi-machine sync.** `saga sync` keeps your personal layer in step across Mac / Linux / Windows over your own private git remote.
- **Versioned, open spec.** [Saga Topic Spec v1.0](docs/spec/saga-topic-v1.md) is published Apache-2.0 and defines the on-disk contract independent of any engine — V8 / ECMAScript model.
- **Local-first, no telemetry.** SQLite index regenerable from markdown. No cloud, no auth, no vendor lock-in.
- **Single static binary.** No runtime, no dependencies. macOS / Linux / Windows × amd64 / arm64.
- **Hardened by default.** Secret-pattern detection at write time (AWS keys, SSH private keys, JWTs, etc.), similarity warnings on near-duplicates, `sensitivity: confidential` opt-out from sync. `gosec`, `govulncheck`, `gitleaks`, `golangci-lint`, CodeQL — all green in CI.

## How memory is organised

```
~/go/bin/saga                    BINARY    one install, used everywhere
~/.saga/personal/                DATA      your private notes (your own git remote)
<project>/.saga/                 DATA      project notes (commits with the project)
```

- **Personal layer** (`~/.saga/personal/`) — identity, preferences, policies, personal topics. Auto-created on first invocation. Sync to *your own* private git repo. Visible from any directory.
- **Project layer** (`<project>/.saga/`) — topics about this codebase. Created with `saga init`. Lives inside the project's git repo and travels with it. Active only when you `cd` into the project.
- **Automatic resolution.** Saga walks up from `cwd` looking for `.saga/meta.yml`, merges with personal. Switch projects, project layer changes; personal stays.

## Install

Pick whichever fits.

### Option A — `go install` (any OS, needs Go ≥ 1.25)

```bash
go install github.com/mopanc/saga/cmd/saga@latest
```

Make sure `$(go env GOPATH)/bin` is on `PATH` (usually `~/go/bin`).

### Option B — prebuilt binary (no Go required)

Every release ships static binaries for all six common platforms. Pick your asset from the [latest release](https://github.com/mopanc/saga/releases/latest):

| OS | Architecture | Asset |
|---|---|---|
| macOS | Intel | `saga_<version>_macos_amd64.tar.gz` |
| macOS | Apple Silicon (M1/M2/M3/M4) | `saga_<version>_macos_arm64.tar.gz` |
| Linux | amd64 (most x86_64 servers/desktops) | `saga_<version>_linux_amd64.tar.gz` |
| Linux | arm64 (Raspberry Pi 4/5, IMX8, AWS Graviton) | `saga_<version>_linux_arm64.tar.gz` |
| Windows | amd64 | `saga_<version>_windows_amd64.zip` |
| Windows | arm64 | `saga_<version>_windows_arm64.zip` |

`checksums.txt` (SHA-256) is published alongside for verification.

#### One-line install — macOS / Linux

```bash
TAG=$(curl -sL https://api.github.com/repos/mopanc/saga/releases/latest | grep -o '"tag_name":\s*"[^"]*"' | cut -d'"' -f4)
OS=$(uname -s | tr '[:upper:]' '[:lower:]' | sed 's/darwin/macos/')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
curl -L "https://github.com/mopanc/saga/releases/download/${TAG}/saga_${TAG#v}_${OS}_${ARCH}.tar.gz" | tar -xz -C /tmp saga
sudo mv /tmp/saga /usr/local/bin/saga
saga version
```

#### One-line install — Windows (PowerShell)

```powershell
$tag = (Invoke-RestMethod https://api.github.com/repos/mopanc/saga/releases/latest).tag_name
$ver = $tag.TrimStart('v')
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$url = "https://github.com/mopanc/saga/releases/download/$tag/saga_${ver}_windows_$arch.zip"
Invoke-WebRequest $url -OutFile $env:TEMP\saga.zip
Expand-Archive -Force $env:TEMP\saga.zip -DestinationPath $HOME\bin
$env:Path = "$HOME\bin;$env:Path"     # add to PATH for the session
saga version
```

### Option C — Homebrew / Scoop

Coming with `v1.0.0` stable. Watch the repo for the announcement.

### Verify

```bash
saga doctor                       # diagnoses everything that is missing
```

`saga doctor` is your compass — run it on any machine and it tells you exactly what is wired, what is missing, and a copy-paste fix for each gap.

## Quick start

Prerequisite: `saga` on your `PATH` (see [Install](#install)).

### 1. Wire into Claude Code

```bash
saga setup-claude --apply         # registers the MCP server and prints the hook snippet
```

Saga has two integration points and they live in different files:

- **MCP server** → `~/.claude.json` (managed by `claude mcp add`). Without this, the `mcp__saga__*` tools never appear in any session.
- **`UserPromptSubmit` hook** → `~/.claude/settings.json`. Without this, Saga does not auto-inject context into prompts.

`--apply` runs `claude mcp add saga -s user -- $(which saga) mcp` for you. The hook snippet must be merged into `settings.json` by hand (automatic edits to that file would risk clobbering other hooks/MCPs).

**Restart Claude Code completely (`Cmd+Q` and reopen)** — MCP servers and settings are read at startup.

### 2. Initialise a project layer (optional)

For each project where you want shared, team-visible notes:

```bash
cd ~/code/acme-platform
saga init                         # creates .saga/meta.yml + .saga/topics/
git add .saga/ && git commit -m "init saga layer"
git push                          # ships with the project
```

`saga init` derives the scope name from `git rev-parse --show-toplevel`. The personal layer is auto-created on first invocation — no manual init needed.

### 3. Validate

```bash
saga doctor                       # everything should be ✓
```

From here, any MCP-configured AI gets the five tools listed below. On Claude Code, the hook injects on every prompt:

- `<saga-meta>` — always, even when the index is empty. Tells the agent that Saga is wired in, lists the tools, and explains when to call `topic_write`. Without this, a fresh session has no way to discover Saga.
- `<saga-identity>` — when profile / preference notes exist (personal baseline).
- `<saga-context>` — when topic notes match the prompt's recall query.

## Multi-machine sync

Saga is built to follow you between PCs. The personal layer is a directory you control — make it a git repo backed by your own private remote, and `saga sync` does the rest.

### One-time setup (first machine)

```bash
cd ~/.saga/personal
git init && git add -A && git commit -m "init"
git branch -M main
git remote add origin git@github.com:<your-user>/saga-personal.git    # private repo
git push -u origin main
```

### Each new machine

```bash
git clone git@github.com:<your-user>/saga-personal.git ~/.saga/personal
saga reindex
```

### Day-to-day

```bash
saga sync             # pull --rebase + push (auto-commits pending changes)
saga sync --status    # report state without changing anything
saga sync --pull      # pull only (useful at session start)
saga sync --push      # push only (useful after a burst of topic_writes)
```

On conflict, `saga sync` stops with clear instructions: resolve manually, `git rebase --continue`, then re-run `saga sync --push`.

`saga doctor` reports the sync state (remote, branch, ahead/behind, uncommitted files).

## CLI

| Command | What it does |
|---|---|
| `saga init` | Create `.saga/meta.yml` and `.saga/topics/` for a project layer in `cwd` |
| `saga reindex` | Rebuild the SQLite index from markdown across active layers |
| `saga sync` | Pull/push the personal layer between machines (`--pull`, `--push`, `--status`, `--no-auto-commit`) |
| `saga lembrancas` | List recent recall events (filters: `--since`, `--kind`, `--topic`) |
| `saga conflicts` | List unresolved `@conflicts_with` topic pairs in active layers |
| `saga show` | Display a topic plus its incoming and outgoing relations |
| `saga capabilities` | Print engine capability declaration (spec version, conformance level, types, operators) |
| `saga lint` | Validate topics against [Saga Topic Spec v1.0](docs/spec/saga-topic-v1.md) — required fields, trait enums, slug ↔ title coherence, relation target resolution, supersedes / derived_from cycles, duplicate ids (`--scope`, `--fix`, `--format json`) |
| `saga doctor` | Diagnose installation, configuration, content, and sync state |
| `saga mcp` | Run as MCP stdio server (invoked by AI clients, not directly) |
| `saga hook` | `UserPromptSubmit` hook for Claude Code (reads event JSON from stdin) |
| `saga setup-claude` | Wire saga into Claude Code (`--apply` registers MCP automatically) |
| `saga version` | Print version |
| `saga help` | Help text |

## MCP tools (every compatible AI client)

| Tool | Purpose |
|---|---|
| `recall` | Retrieve topic notes (FTS5 + BM25 + recency + relation-aware ranking) across active scopes |
| `topic_read` | Read a single topic note in full by name (slug or title) |
| `topic_list` | List topic notes visible in the current scope |
| `topic_write` | Create or update a topic note (default scope=personal; modes: `create` / `append` / `replace`). Blocks credential-shaped content by default; warns on near-duplicate titles |
| `lembranca_log` | Inspect the recall history (filters: `since`, `kind`, `topic`, `limit`) |
| `saga_capabilities` | Engine capability declaration for capability-negotiation (spec §10) |

## Performance & limits

Saga is designed so per-prompt token cost stays **constant** as your memory grows. The size of the corpus does not change how much context is injected — only retrieval ranking does.

### Per-prompt injection — hard caps in code

| Block | Cap | Source of truth |
|---|---|---|
| `<saga-meta>` | ~80 tokens (always-on) | `cmd/saga/cmd_hook.go::emitMetaBlock` |
| `<saga-identity>` | **400 tokens** (env-overridable via `SAGA_BASELINE_MAX_TOKENS`) | `internal/saga/baseline.go::DefaultBaselineMaxTokens` |
| `<saga-context>` | top-3 topics × 1000 chars each (~750 tokens), with **8 KB total ceiling** | `cmd/saga/cmd_hook.go::hookTopK / maxTopicBodyChars / maxHookOutputBytes` |
| **Total per prompt** | **≈ 2000 tokens, hard ceiling** | — |

Topics longer than the per-snippet cap are truncated with a `[truncated — call mcp__saga__topic_read for full body]` marker; the AI fetches the rest only when worth it.

### Per-write cap — `topic_write` body

| Limit | Value | Source |
|---|---|---|
| Per-topic body | **8000 chars (~2000 tokens, ~3000 words)** | `internal/saga/service.go::MaxTopicBodyChars` |

Writes above the cap are rejected with an actionable error (split into narrower topics, or `mode=append` for evolving knowledge with dated sections). This keeps `topic_read` results bounded too — no surprise 13 KB tool result.

### What grows with the corpus

| Metric | Growth | Practical bound |
|---|---|---|
| Tokens injected per prompt | **constant (≈ 2000)** | n/a — by design |
| Reindex time (full) | ~30 ms / topic | 10k topics ≈ 5 min, run on demand |
| Recall latency (FTS5 + BM25) | sub-linear | well under 200 ms for 100k topics in measurements |
| Index DB size | ~30 % overhead over markdown | 10k notes × 2 KB ≈ 26 MB total |
| `git pull/push` of personal layer | linear in history | use `--depth=1` clone for very large layers |

### What can degrade — and the recovery path

1. **Hook timeout.** Claude Code allows ~30 s for `UserPromptSubmit`. Recall on extreme corpora can flirt with this limit. Mitigation in roadmap: `sqlite-vec` extension + Ollama embeddings (Phase 1.5).
2. **Reindex failure.** The index is fully regenerable from markdown. `rm $SAGA_HOME/index.db && saga reindex` recovers 100 % of state.
3. **Sync conflict.** Surfaced by `saga sync` with the conflicting file paths and exit-non-zero. Resolve with `git rebase --continue` and `saga sync --push`.
4. **Wide recall query.** Top-K is bounded; ranking still applies. Worst case is irrelevant snippets, never an injection blow-up.

### Token budget visibility (planned)

`saga doctor` will surface a 7-day token-budget summary (avg/p99 injected, hook truncation count, `topic_read` average size). Backed by the existing `lembrança` log; landing in a future release.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `SAGA_HOME` | `~/.saga` | Personal layer + index.db location |
| `SAGA_DB_PATH` | `$SAGA_HOME/index.db` | Override the index path |
| `SAGA_BASELINE_MAX_TOKENS` | `400` | Token budget for `<saga-identity>` injection per prompt |

## Tested clients

| Client | MCP | Auto-injection hook |
|---|---|---|
| [Claude Code](https://claude.com/claude-code) | ✓ | ✓ (`UserPromptSubmit`) |
| [Cursor](https://cursor.com) | ✓ | — (manual `recall` / `topic_read` calls) |
| [Windsurf](https://windsurf.com) | ✓ | — |
| [Antigravity](https://antigravity.google.com) | ✓ | — |

Every MCP-conformant client should work; auto-injection on each prompt is Claude Code-only because no other client exposes a comparable hook.

## Troubleshooting

**`saga: command not found` in one terminal but works in another.**
Often an IDE (Cursor / VS Code / Antigravity) opens a login shell that reads `.zprofile` but not `.zshrc`. Add the `PATH` export to both files.

**Hook does not fire after editing `settings.json`.**
Claude Code does not reload settings at runtime. `Cmd+Q` and reopen.

**`saga doctor` reports "saga MCP server not registered".**
Claude Code reads MCP servers from `~/.claude.json` (not `~/.claude/settings.json`). Run `saga setup-claude --apply`, or manually `claude mcp add saga -s user -- $(which saga) mcp`. If the `claude` CLI is missing, install / repair Claude Code first.

**`saga doctor` reports "UserPromptSubmit hook not wired".**
You did not merge the hook snippet into `~/.claude/settings.json`. Re-run `saga setup-claude` and add the `hooks` block without removing any existing entries.

**`saga reindex` reports `failed=N`.**
Invalid YAML frontmatter in some `.md` files. Run `saga reindex` to see exact paths; open and validate the YAML.

## Architecture

| Layer | Choice |
|---|---|
| Language | Go 1.25+ |
| MCP transport | JSON-RPC 2.0 stdio (in-tree, ~200 LOC, no third-party MCP SDK) |
| Index | SQLite via [`modernc.org/sqlite`](https://modernc.org/sqlite) (pure Go, no CGO, cross-compiles trivially) |
| Search | FTS5 + BM25 + custom recency boost; `sanitizeFTSQuery` against keyword injection |
| Vector (Phase 1.5) | `sqlite-vec` (lazy-loaded; `embedding BLOB` column reserved from day one) |
| Embeddings (Phase 1.5) | Ollama local, `nomic-embed-text` |

```
saga/
├── cmd/saga/                  single binary, subcommands in cmd_*.go
├── internal/
│   ├── saga/                  core: parser, indexer, layers, service, baseline, lembrança, sync
│   │   └── migrations/*.sql   schema embedded in the binary
│   └── mcp/                   JSON-RPC 2.0 stdio server
└── docs/                      design notes
```

## Saga Topic Spec

Saga is a **cognitive substrate over markdown**, not just a memory tool. The on-disk contract — primitives, relations, operators, conformance levels — is published as a versioned specification, separate from any particular engine.

- [`docs/spec/saga-topic-v1.md`](docs/spec/saga-topic-v1.md) — Saga Topic Specification v1.0 (draft).
- [`docs/spec/README.md`](docs/spec/README.md) — spec/engine separation, versioning, contribution process.

The model is ECMAScript / V8: an open spec that anyone can implement, plus a canonical reference engine (this repository) that catches up to the spec over time. Other engines — IDE lint extensions, cognitive runtimes, hosted services — are welcome and conform by implementing the level (§10) that fits their surface.

## Documentation

- [docs/spec/saga-topic-v1.md](docs/spec/saga-topic-v1.md) — **Saga Topic Spec v1.0** (normative on-disk contract).
- [docs/DESIGN_v2.md](docs/DESIGN_v2.md) — technical architecture (storage, MCP, SQL).
- [docs/COGNITIVE_MODEL.md](docs/COGNITIVE_MODEL.md) — cognitive model (5 layers + 2 cross-cutting); failure modes, anti-creep.
- [docs/PLAN.md](docs/PLAN.md) — iterations and utility tests.
- [docs/ROADMAP_v2.md](docs/ROADMAP_v2.md) — granular tasks with acceptance criteria.

## Building from source

```bash
go build ./...      # compiles all packages
go test ./...       # runs the test suite
go run ./cmd/saga version
```

Main branch: `main`.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the conventions, [SECURITY.md](SECURITY.md) for vulnerability reporting, and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for community expectations.

## License

[Apache-2.0](LICENSE) © 2026 Jorge Morais
