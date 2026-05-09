# Saga — persistent memory for AI coding agents

[![CI](https://github.com/mopanc/saga/actions/workflows/ci.yml/badge.svg)](https://github.com/mopanc/saga/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/mopanc/saga?include_prereleases&sort=semver)](https://github.com/mopanc/saga/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/mopanc/saga.svg)](https://pkg.go.dev/github.com/mopanc/saga)
[![Go Report Card](https://goreportcard.com/badge/github.com/mopanc/saga)](https://goreportcard.com/report/github.com/mopanc/saga)
[![License: Apache-2.0](https://img.shields.io/github/license/mopanc/saga)](LICENSE)
[![CodeQL](https://github.com/mopanc/saga/actions/workflows/codeql.yml/badge.svg)](https://github.com/mopanc/saga/actions/workflows/codeql.yml)

> **Saga** is a topic-grained, layered memory layer for AI coding agents. It lets Claude Code, Cursor, Windsurf, Antigravity and any other [Model Context Protocol](https://modelcontextprotocol.io) (MCP) client remember what they learned across sessions — your codebase, your decisions, your preferences — so the next conversation starts where the last one ended.

Local-first. Single static binary. Markdown notes in git, indexed by SQLite. Cross-IDE, cross-machine.

## Why Saga

AI coding agents have no memory between sessions. Every conversation starts blank — you re-explain your stack, your conventions, the bug you fixed last week. For solo developers and small teams working on real codebases, this re-explanation costs hours every week.

Saga gives the agent a place to **write durable notes** after an investigation, and a fast retrieval path to **read them back** on the next session. The unit of memory is a curated *topic note* (~500 words, self-contained markdown), not a chunked sentence corpus — because investigation work needs coherent context, not scattered phrases.

## Highlights

- **Topic-grained, not chunk-grained.** ~500-word self-contained notes beat sentence-level chunking for code-investigation workloads.
- **Layered scopes.** `personal | project | dept | org` — each layer has an independent owner, sync remote, and sensitivity. Personal travels with you; project travels with the repo.
- **Cross-IDE via MCP.** Five tools (`recall`, `topic_read`, `topic_list`, `topic_write`, `lembranca_log`) exposed over JSON-RPC 2.0 stdio to any MCP client.
- **Auto-injection on Claude Code.** A `UserPromptSubmit` hook surfaces the relevant topic notes on every prompt so the agent never forgets to look.
- **Multi-machine sync.** `saga sync` keeps your personal layer in step across Mac / Linux / Windows over your own private git remote.
- **Local-first, no telemetry.** SQLite index regenerable from markdown. No cloud, no auth, no vendor lock-in.
- **Single static binary.** No runtime, no dependencies. macOS / Linux / Windows × amd64 / arm64.
- **Security-hardened.** `gosec`, `govulncheck`, `gitleaks`, `golangci-lint`, CodeQL — all green in CI.

## How memory is organised

```
~/go/bin/saga                    BINARY    one install, used everywhere
~/.saga/personal/                DATA      your private notes (your own git remote)
<project>/.saga/                 DATA      project notes (commits with the project)
```

- **Personal layer** (`~/.saga/personal/`) — identity, preferences, policies, personal topics. Auto-created on first invocation. Sync to *your own* private git repo. Visible from any directory.
- **Project layer** (`<project>/.saga/`) — topics about this codebase. Created with `saga init`. Lives inside the project's git repo and travels with it. Active only when you `cd` into the project.
- **Automatic resolution.** Saga walks up from `cwd` looking for `.saga/meta.yml`, merges with personal. Switch projects, project layer changes; personal stays.

## Quick start

Prerequisite: Go ≥ 1.25 (or skip the Go install and use the prebuilt binary).

### 1. Install

```bash
go install github.com/mopanc/saga/cmd/saga@latest
```

Or, no Go installed? Grab a prebuilt binary from the [latest release](https://github.com/mopanc/saga/releases) (macOS / Linux / Windows × amd64 / arm64):

```bash
gh release download -R mopanc/saga -p '*macos_arm64*' --output - | tar -xz -C ~/go/bin saga
```

Make sure `~/go/bin` is in `PATH`:

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zprofile  # covers IDE terminals too
source ~/.zshrc
saga doctor                       # diagnoses everything that is missing
```

### 2. Wire into Claude Code

```bash
saga setup-claude --apply         # registers the MCP server and prints the hook snippet
```

Saga has two integration points and they live in different files:

- **MCP server** → `~/.claude.json` (managed by `claude mcp add`). Without this, the `mcp__saga__*` tools never appear in any session.
- **`UserPromptSubmit` hook** → `~/.claude/settings.json`. Without this, Saga does not auto-inject context into prompts.

`--apply` runs `claude mcp add saga -s user -- $(which saga) mcp` for you. The hook snippet must be merged into `settings.json` by hand (automatic edits to that file would risk clobbering other hooks/MCPs).

**Restart Claude Code completely (`Cmd+Q` and reopen)** — MCP servers and settings are read at startup.

### 3. Initialise a project layer (optional)

For each project where you want shared, team-visible notes:

```bash
cd ~/code/acme-platform
saga init                         # creates .saga/meta.yml + .saga/topics/
git add .saga/ && git commit -m "init saga layer"
git push                          # ships with the project
```

`saga init` derives the scope name from `git rev-parse --show-toplevel`. The personal layer is auto-created on first invocation — no manual init needed.

### 4. Validate

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
| `saga doctor` | Diagnose installation, configuration, content, and sync state |
| `saga mcp` | Run as MCP stdio server (invoked by AI clients, not directly) |
| `saga hook` | `UserPromptSubmit` hook for Claude Code (reads event JSON from stdin) |
| `saga setup-claude` | Wire saga into Claude Code (`--apply` registers MCP automatically) |
| `saga version` | Print version |
| `saga help` | Help text |

## MCP tools (every compatible AI client)

| Tool | Purpose |
|---|---|
| `recall` | Retrieve topic notes (FTS5 + BM25 + recency boost) across active scopes |
| `topic_read` | Read a single topic note in full by name (slug or title) |
| `topic_list` | List topic notes visible in the current scope |
| `topic_write` | Create or update a topic note (default scope=personal; modes: `create` / `append` / `replace`) |
| `lembranca_log` | Inspect the recall history (filters: `since`, `kind`, `topic`, `limit`) |

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

## Documentation

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
