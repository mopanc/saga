# Saga — AI Investigation Memory

Camada de conhecimento layered, local-first, partilhada entre IAs (Claude Code, Cursor, Windsurf, Antigravity) via MCP. Topic notes em markdown, indexadas por SQLite, sincronizadas via git. O LLM lê **e** mantém as notas — a próxima conversa começa com o que a anterior descobriu.

## Filosofia

1. **Investigation memory, não knowledge dump.** A unidade é a *topic note* curada (~500 palavras, auto-contida), não a frase indexada por BM25.
2. **Layered scopes.** `personal | project | dept | org` — cada camada tem owner independente. Default escreve em `personal`; promoção para scopes mais altos é explícita.
3. **Substrato sobre invenção.** git para sync/versionamento/ACL. SQLite para índice (regenerável). Markdown para fonte de verdade. Saga não inventa nenhum destes.

## Quick start

Pré-requisito: Go ≥ 1.22.

```bash
# Instalar (uma vez)
go install github.com/mopanc/saga/cmd/saga@latest
# ou, do source: go build -o ~/bin/saga ./cmd/saga

# Em cada projeto onde queres memória partilhada com a equipa:
cd ~/code/acme-platform
saga init                        # cria .saga/meta.yml + .saga/topics/

# Wire no Claude Code (uma vez):
saga setup-claude                # imprime snippet para ~/.claude/settings.json
```

`saga init` detecta o nome do projeto a partir do `git rev-parse --show-toplevel` (fallback: basename do cwd). Depois disso, qualquer IA com MCP que aponte para `saga mcp` ganha as 4 tools (`recall`, `topic_read`, `topic_list`, `topic_write`); o hook em Claude Code injecta automaticamente os top-3 tópicos relevantes a cada prompt.

Por default, índice + personal layer em `~/.saga/`. Override via `SAGA_HOME`.

## Subcomandos

| Comando | Para quê |
|---|---|
| `saga init` | Cria `.saga/meta.yml` + `.saga/topics/` no cwd |
| `saga reindex` | Reconstrói o índice SQLite a partir dos `.md` das layers activas |
| `saga mcp` | Corre como MCP stdio server (chamado por AI clients) |
| `saga hook` | Hook UserPromptSubmit para Claude Code (recebe event JSON em stdin) |
| `saga setup-claude` | Imprime o JSON para colares em `~/.claude/settings.json` |
| `saga version` | Imprime versão |

## Documentação

- [docs/DESIGN_v2.md](docs/DESIGN_v2.md) — spec actual (topic-grained RAG, layered scopes, Go).
- [docs/DESIGN.md](docs/DESIGN.md) — v1 histórica (memória de frases). Mantida para referência.
- [docs/ROADMAP.md](docs/ROADMAP.md) — v1 histórica. Roadmap revisto vive em `DESIGN_v2.md §17`.

## Stack (LOCKED)

| Componente | Escolha |
|---|---|
| Linguagem | Go 1.22+ |
| MCP transport | JSON-RPC 2.0 stdio (implementação interna em `internal/mcp/`) |
| SQLite | `modernc.org/sqlite` (puro Go, sem CGO) |
| Vector (Phase 1.5) | `sqlite-vec` extension |
| Embeddings (Phase 1.5) | Ollama local, `nomic-embed-text` |

## Layout

```
saga/
├── cmd/saga/                  # single binary, subcomandos em cmd_*.go
├── internal/
│   ├── saga/                  # core: parser, indexer, layers, service
│   │   └── migrations/*.sql   # schema embedded em binário
│   └── mcp/                   # JSON-RPC 2.0 stdio server (~200 LOC)
└── docs/
```

## Para developers

```bash
go build ./...      # compila tudo
go test ./...       # corre testes (39 actuais)
go run ./cmd/saga version
```

Branch principal de desenvolvimento neste momento: `pivot/v2-go`.
