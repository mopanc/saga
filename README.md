# Saga — AI Investigation Memory

Camada de conhecimento layered, local-first, partilhada entre IAs (Claude Code, Cursor, Windsurf, Antigravity) via MCP. Topic notes em markdown, indexadas por SQLite, sincronizadas via git. O LLM lê **e** mantém as notas — a próxima conversa começa com o que a anterior descobriu.

**Estado:** v2 em pivot. Branch `pivot/v2-go` — re-escrita Go a partir do scaffold inicial em TypeScript.

## Filosofia em três pontos

1. **Investigation memory, não knowledge dump.** A unidade é a *topic note* curada (~500 palavras, auto-contida), não a frase indexada por BM25.
2. **Layered scopes.** `personal | project | dept | org` — cada camada tem owner independente. Default escreve em `personal`; promoção para scopes mais altos é explícita.
3. **Substrato sobre invenção.** git para sync/versionamento/ACL. SQLite para índice (regenerável). Markdown para fonte de verdade. Saga não inventa nenhum destes.

## Documentação

- [DESIGN_v2.md](docs/DESIGN_v2.md) — spec actual (topic-grained RAG, layered scopes, Go).
- [DESIGN.md](docs/DESIGN.md) — v1 histórica (memória de frases). Mantida para referência.
- [ROADMAP.md](docs/ROADMAP.md) — v1 histórica. Roadmap revisto vive em `DESIGN_v2.md §17`.

## Stack (LOCKED)

| Componente | Escolha |
|---|---|
| Linguagem | Go 1.22+ |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` (a integrar) |
| SQLite | `modernc.org/sqlite` (puro Go, sem CGO) |
| Vector (Phase 1.5) | `sqlite-vec` extension |
| Embeddings (Phase 1.5) | Ollama local, `nomic-embed-text` |

## Quick start

Pré-requisitos: Go ≥ 1.22.

```bash
go build ./...
go test ./...

# Smoke test
go run ./cmd/saga version
go run ./cmd/saga reindex
```

Por default, dados em `~/.saga/`. Override via `SAGA_HOME`.

## Layout

```
saga/
├── cmd/
│   ├── saga/          # CLI
│   ├── saga-mcp/      # MCP stdio server
│   └── saga-hook/     # Claude Code UserPromptSubmit hook
├── internal/saga/     # core: DB, parser, retrieval, layers
│   └── migrations/    # SQL embebido em binário
└── docs/
```

## Próximo passo

Implementar o resolver de layers (cwd → walk-up → meta.yml → inherits) e os tools MCP `recall`, `topic.read`, `topic.list`, `topic.write`. Ver `docs/DESIGN_v2.md §12-14`.
