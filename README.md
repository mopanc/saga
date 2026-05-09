# Saga — AI Investigation Memory

Camada de conhecimento layered, local-first, partilhada entre IAs (Claude Code, Cursor, Windsurf, Antigravity) via MCP. Topic notes em markdown, indexadas por SQLite, sincronizadas via git. O LLM lê **e** mantém as notas — a próxima conversa começa com o que a anterior descobriu.

## Filosofia

1. **Investigation memory, não knowledge dump.** A unidade é a *topic note* curada (~500 palavras, auto-contida), não a frase indexada por BM25.
2. **Layered scopes.** `personal | project | dept | org` — cada camada tem owner independente. Default escreve em `personal`; promoção para scopes mais altos é explícita.
3. **Substrato sobre invenção.** git para sync/versionamento/ACL. SQLite para índice (regenerável). Markdown para fonte de verdade. Saga não inventa nenhum destes.

## Os dois sítios onde os dados vivem

A separação é simples e ortogonal — vale a pena fixar:

```
~/go/bin/saga                    BINÁRIO   global, uma instalação
~/.saga/personal/                DADOS     teus pessoais (privados, teu git)
<projeto>/.saga/                 DADOS     do projecto (parte do git do projecto)
```

- **Personal layer** (`~/.saga/personal/`) — identidade, preferências, política, tópicos pessoais. Auto-criada na 1ª invocação. Sincronizas com **TEU** git remote privado. Visível em qualquer projecto.
- **Project layer** (`<projeto>/.saga/`) — tópicos sobre este código. Criada com `saga init`. Faz parte do git do projecto, viaja com ele. Activa só quando estás cd'd dentro.
- **Resolução automática:** o hook olha para `cwd`, faz walk-up para `.saga/meta.yml`, junta com personal. Mudas de projecto, project muda; personal fica.

## Quick start

Pré-requisito: Go ≥ 1.22.

### 1. Instalar (uma vez)

```bash
go install github.com/mopanc/saga/cmd/saga@latest
```

Alternativa sem Go: baixar o binário pré-compilado da [última release](https://github.com/mopanc/saga/releases/latest) (macOS/Linux/Windows × amd64/arm64) e mover para um sítio no PATH:

```bash
gh release download -R mopanc/saga -p '*macos_arm64*' --output - | tar -xz -C ~/go/bin saga
```

### 2. Garantir que o PATH inclui `~/go/bin`

```bash
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zprofile  # cobre também IDE terminals
source ~/.zshrc
saga doctor                     # diagnostica tudo o que falta
```

`saga doctor` é a tua bússola — corre em qualquer máquina e diz-te exactamente o que está montado, o que falta, e como corrigir cada coisa.

### 3. Wire no Claude Code (uma vez)

```bash
saga setup-claude --apply        # regista o MCP e imprime o snippet do hook
```

Saga tem dois pontos de integração e vivem em ficheiros diferentes:

- **MCP server** → `~/.claude.json` (gerido pelo `claude mcp add`). Sem isto, as ferramentas `mcp__saga__*` não aparecem em sessão nenhuma.
- **UserPromptSubmit hook** → `~/.claude/settings.json`. Sem isto, o saga não injecta contexto nos prompts.

`--apply` corre `claude mcp add saga -s user -- $(which saga) mcp` por ti. O snippet do hook ainda tens de fazer merge à mão (não é seguro mexer no `settings.json` automaticamente — pode ter outros hooks/MCPs).

**Reinicia o Claude Code completamente (`Cmd+Q` + reabrir)** — MCP servers e settings só são lidos em arranque.

### 4. Inicializar a Saga em cada projecto onde queres memória de equipa

```bash
cd ~/code/acme-platform
saga init                       # cria .saga/meta.yml + .saga/topics/
git add .saga/ && git commit -m "init saga layer"
git push                        # vai com o projecto
```

`saga init` detecta o nome do projecto via `git rev-parse --show-toplevel`. Personal layer é auto-criada na primeira invocação — não precisa de init manual.

### 5. Validar

```bash
saga doctor                     # devia mostrar tudo ✓
```

A partir daqui, qualquer IA configurada com MCP recebe 5 tools (`recall`, `topic_read`, `topic_list`, `topic_write`, `lembranca_log`); o hook em Claude Code injecta automaticamente em cada prompt:

- `<saga-meta>` — sempre, mesmo com saga vazio. Diz à IA que o saga existe, lista os tools, e quando chamar `topic_write`. Sem isto, uma sessão fresca não tem como descobrir que o saga está montado.
- `<saga-identity>` — quando há profile/preference notes
- `<saga-context>` — quando topic notes batem na query

## Subcomandos

| Comando | Para quê |
|---|---|
| `saga init` | Cria `.saga/meta.yml` + `.saga/topics/` no cwd |
| `saga reindex` | Reconstrói o índice SQLite a partir dos `.md` das layers activas |
| `saga lembrancas` | Lista eventos de recall recentes (filtros: `--since`, `--kind`, `--topic`) |
| `saga doctor` | Diagnostica instalação, config, e estado do conteúdo |
| `saga mcp` | Corre como MCP stdio server (chamado por AI clients) |
| `saga hook` | Hook UserPromptSubmit para Claude Code (recebe event JSON em stdin) |
| `saga setup-claude` | Wire no Claude Code (MCP via `claude mcp add` + hook em `settings.json`); usa `--apply` para registar o MCP automaticamente |
| `saga version` | Imprime versão |
| `saga help` | Lista comandos |

## MCP tools (visíveis para qualquer IA)

| Tool | Função |
|---|---|
| `recall` | Search FTS5 + BM25 + recency boost por scopes activos |
| `topic_read` | Lê topic note inteira por nome (slug ou título) |
| `topic_list` | Lista tópicos visíveis no scope actual |
| `topic_write` | Cria/actualiza topic note (default scope=personal, modes: create/append/replace) |
| `lembranca_log` | Inspecciona histórico de recall (filtros: since/kind/topic/limit) |

## Variáveis de ambiente

| Variável | Default | O que faz |
|---|---|---|
| `SAGA_HOME` | `~/.saga` | Localização do personal layer + index.db |
| `SAGA_DB_PATH` | `$SAGA_HOME/index.db` | Override do path do índice |
| `SAGA_BASELINE_MAX_TOKENS` | `400` | Limite de tokens injectados no `<saga-identity>` por prompt |

## Troubleshooting

**`saga: command not found` num terminal mas funciona noutro.**
Tipicamente o IDE (Cursor / VS Code / Antigravity) abre login shell que lê `.zprofile` mas não `.zshrc`. Adiciona o export aos dois ficheiros (ver Quick Start §2).

**Hook não dispara depois de configurar settings.json.**
O Claude Code não recarrega settings em runtime. Tens que `Cmd+Q` e reabrir.

**`saga doctor` diz "saga MCP server not registered".**
O Claude Code lê MCP servers de `~/.claude.json` (não de `~/.claude/settings.json`). Corre `saga setup-claude --apply` ou manualmente `claude mcp add saga -s user -- $(which saga) mcp`. Se o `claude` CLI não estiver no PATH, instala/repara o Claude Code primeiro.

**`saga doctor` diz "UserPromptSubmit hook not wired".**
Não fizeste o merge do snippet do hook no `~/.claude/settings.json`. Re-corre `saga setup-claude` e mete a secção `hooks` sem apagar outras que já tenhas.

**`saga reindex` reporta `failed=N` em alguns ficheiros.**
Frontmatter inválido em `.md` files. Corre `saga reindex` em modo verbose para ver paths exactos; abre o ficheiro e valida YAML.

## Documentação

- [docs/DESIGN_v2.md](docs/DESIGN_v2.md) — arquitectura técnica (storage, MCP, SQL).
- [docs/COGNITIVE_MODEL.md](docs/COGNITIVE_MODEL.md) — modelo cognitivo (5 camadas + 2 transversais), erros evitados, anti-creep.
- [docs/PLAN.md](docs/PLAN.md) — iterações e testes de utilidade.
- [docs/ROADMAP_v2.md](docs/ROADMAP_v2.md) — tasks granulares com critérios e esforço.
- [docs/DESIGN.md](docs/DESIGN.md) + [docs/ROADMAP.md](docs/ROADMAP.md) — v1 histórica, mantidas para referência.

## Stack (LOCKED)

| Componente | Escolha |
|---|---|
| Linguagem | Go 1.22+ |
| MCP transport | JSON-RPC 2.0 stdio (implementação interna em `internal/mcp/`, ~200 LOC) |
| SQLite | `modernc.org/sqlite` (puro Go, sem CGO; cross-compile trivial) |
| Vector (Phase 1.5) | `sqlite-vec` extension (carregado lazy; `embedding BLOB` reservado desde dia 1) |
| Embeddings (Phase 1.5) | Ollama local, `nomic-embed-text` |

## Layout

```
saga/
├── cmd/saga/                  # single binary, subcomandos em cmd_*.go
├── internal/
│   ├── saga/                  # core: parser, indexer, layers, service, baseline, lembrança
│   │   └── migrations/*.sql   # schema embedded em binário
│   └── mcp/                   # JSON-RPC 2.0 stdio server
└── docs/
```

## Para developers

```bash
go build ./...      # compila tudo
go test ./...       # corre testes (55 actuais)
go run ./cmd/saga version
```

Branch principal: `main`.
