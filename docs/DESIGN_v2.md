# Saga — DESIGN v2

> Pivot a partir de `DESIGN.md` (v1). Refina, não revoga, as decisões LOCKED de v1 que continuam válidas (single-tenant, local-first, taxonomia emerge dos dados, datas auditam não decidem). Substitui o que mudou — sobretudo: o que é a unidade primária (passou de *frase* para *topic note*) e onde vive (passou de tabela única para *ficheiros markdown em git, indexados por SQLite*).

## 0. Contexto

A v1 desenhou uma "memória pessoal genérica" — uma tabela de frases curtas indexadas por BM25. A v2 reconhece três coisas que a v1 não viu:

1. O problema real é **investigation memory** — a IA paga novamente o custo de descoberta a cada sessão. A unidade que evita isto é a **topic note** auto-contida, não a frase.
2. O conhecimento tem **scope natural**: pessoal, projeto, departamento, organização. Cada scope tem owner, lifecycle e segurança independentes.
3. Memória, política, identidade e factos são **tipos diferentes** com regras de merge diferentes. Tratar tudo como `text + tags` força ambiguidade.

## 1. Visão (revista)

> Saga é uma camada de conhecimento layered, local-first, que vive **dentro** dos repositórios do trabalho. Cada IA com que trabalhas (Claude Code, Cursor, Windsurf, Antigravity) consulta a mesma camada via MCP. O conhecimento promove-se entre scopes (personal → project → dept → org) explicitamente, deixando rasto auditável em git.

## 2. Princípios (LOCKED)

1. **Invisibilidade.** O sistema só funciona se melhorar o raciocínio sem exigir gestão activa.
2. **Substrato sobre invenção.** Sempre que git, OS filesystem ou SQLite resolvem o problema, o kernel não inventa. Sync é git pull. Permissões são chmod. Indexação é FTS5.
3. **Markdown é a fonte; SQLite é índice descartável.** A perda do `.db` é inconveniência, não tragédia.
4. **Default escreve em `personal`. Promoção é explícita.** Privacidade por default, partilha por intenção.
5. **Uma ideia por kernel.** Memória é Saga. Runtime gateway, dashboards, telemetria — produtos *separados* que compõem via MCP.
6. **Validation by use.** Phase só avança com evidência da phase anterior em uso real.

## 3. Arquitectura (alto nível)

```
   ┌──────────────────────────────────────────────────────┐
   │  saga-mcp (Go binary, stdio MCP server)              │
   │                                                       │
   │  ┌──────────┐   ┌──────────┐   ┌──────────┐          │
   │  │ resolver │ → │  reader  │ → │  merger  │          │
   │  │ (cwd →   │   │ (per     │   │ (typed   │          │
   │  │  layers) │   │  layer)  │   │  rules)  │          │
   │  └──────────┘   └──────────┘   └──────────┘          │
   │                       │                              │
   │                ┌──────┴───────┐                      │
   │                ▼              ▼                      │
   │         ┌───────────┐   ┌──────────┐                 │
   │         │ md files  │   │ sqlite   │                 │
   │         │ (truth)   │   │ (index)  │                 │
   │         └───────────┘   └──────────┘                 │
   └──────────────────────────────────────────────────────┘
        │                        │
        ▼                        ▼
   ┌─────────┐              ┌─────────┐
   │ git ops │              │ ollama  │
   │ (sync,  │              │ (embed, │
   │ commit) │              │ Phase   │
   │         │              │  1.5)   │
   └─────────┘              └─────────┘
```

**Frontends (consumers):**

- `saga-hook` — binário invocado em `UserPromptSubmit`; lê stdin, chama o resolver+reader, escreve bloco de contexto em stdout.
- `saga-mcp` — MCP stdio server invocado por Claude Code, Cursor, etc.
- `saga` — CLI manual (`saga topic list`, `saga lint`, `saga export`, etc).

Os três partilham o mesmo *core package* em Go.

## 4. Stack (LOCKED)

| Componente | Escolha | Notas |
|---|---|---|
| Linguagem | **Go 1.22+** | Cold-start <10ms, single binary, cross-compile trivial |
| MCP SDK | `github.com/modelcontextprotocol/go-sdk` | Oficial Anthropic |
| SQLite | `modernc.org/sqlite` | Port puro Go, sem CGO; cross-compile sem dor |
| FTS | FTS5 nativo (built-in SQLite) | Tokenizer `unicode61 remove_diacritics 2` |
| Vector (Phase 1.5) | `sqlite-vec` extension | Carrega via `LoadExtension`; mesmo `.db` |
| Embeddings (Phase 1.5) | Ollama local, `nomic-embed-text` | API `/api/embeddings`; default zero-cost |
| Git ops | `os/exec` invocando `git` | Não vincular libgit2 — fricção desnecessária |
| Crypto (Phase 2) | `filippo.io/age` | Encryption do personal layer quando sincronizado |
| YAML frontmatter | `gopkg.in/yaml.v3` | Parsing de frontmatter das notas |
| Build | `goreleaser` | Distribuição mac/linux/windows num comando |

## 5. Camadas (layered scopes)

| Scope | Identifier | Owner | Storage | Sync |
|---|---|---|---|---|
| Personal | `personal` | utilizador | `~/.saga/personal/` | git opt-in (privado, encriptado com age) |
| Project | `project:<name>` | equipa do projeto | `<repo-root>/.saga/` | git nativo do projeto |
| Department | `dept:<name>` | departamento | `~/.saga/cache/dept-<name>/` | git pull-on-boot do remote configurado |
| Org | `org:<name>` | organização | `~/.saga/cache/org-<name>/` | idem |

**Discovery do project layer:** `saga-mcp` ao arrancar lê `cwd` (passado pelo cliente MCP), faz walk-up até encontrar directório `.saga/` com `meta.yml`. Esse é o project layer activo. Se não encontrar, project layer fica vazio.

**Inherits:** o `meta.yml` do project layer pode declarar `inherits: [dept:bm, org:bm]`. O resolver carrega esses layers automaticamente.

## 6. Tipologia de conhecimento

| Tipo | Comportamento de merge | Onde vive |
|---|---|---|
| `profile` | Apenas `personal`. Outras layers ignoradas. | `personal/profile/*.md` |
| `preference` | `personal` por default; `project` pode override com `overrides: [pref-name]`. | qualquer layer |
| `policy` | Mais específico ganha (`project > dept > org`). `personal` não pode override. | `project|dept|org/policy/*.md` |
| `topic` | Sem merge. Namespaced por scope. Recall pode devolver tópicos de N scopes. | qualquer layer, `*/topics/*.md` |

Esta tipologia é o que torna o merge previsível em vez de mágico. Sem ela, o sistema é adivinhação.

## 7. Layout de directorias

**Personal layer (~/.saga/personal/):**

```
~/.saga/
├── config.yml                      # configuração global do utilizador
├── personal/                       # git repo (sempre)
│   ├── meta.yml
│   ├── profile/
│   │   └── identity.md
│   ├── preferences/
│   │   └── code-style.md
│   └── topics/
│       └── *.md                    # notas pessoais (rascunhos, ainda não promovidas)
└── cache/
    ├── dept-bm/                    # clone read-mostly
    └── org-bm/                     # idem
```

**Project layer (vive no repo do projeto):**

```
~/code/acme-platform/                      # repo do projeto
├── .saga/
│   ├── meta.yml
│   ├── policy/                     # branching, code style do projeto
│   ├── preferences/                # opcional, overrides de preferences
│   └── topics/
│       ├── mjpeg-performance.md
│       ├── go2rtc-architecture.md
│       └── hardware-variants.md
└── src/
```

## 8. Topic note — schema

Frontmatter YAML + corpo Markdown livre. Frontmatter é normativo; corpo é narrativa.

```markdown
---
id: 01HXY5KZQVJ8M3R7ABCDEFGHIJ              # ULID, gerado uma vez
scope: project:acme-platform                        # scope do file (matches layer)
type: topic                                  # profile | preference | policy | topic
title: MJPEG performance                     # human-readable, único dentro do scope
synonyms:                                    # para matching de queries
  - mjpeg slow
  - stream lento
  - go2rtc lentidão
  - video laggy
sensitivity: internal                        # public | internal | confidential
confidence: validated                        # proposed | validated
created_at: 2026-04-12T10:30:00Z
updated_at: 2026-04-20T15:45:00Z
created_by: jorge@example.com
updated_by: claude-code
references:                                  # para staleness tracking
  - path: controllers/stream.go
    lines: "120-180"
    blame_hash: a3f7d2c8e1b9
  - path: services/mjpeg/handler.go
    blame_hash: 9b1e4f8a7c2d
related:
  - project:acme-platform:go2rtc-architecture
  - project:acme-platform:socket-protocol
---

## Sumário
MJPEG é servido por handler dedicado, separado do go2rtc (que neste projeto
trata só RTCP). A latência observada no Pi 4 é ~200ms; o objectivo é <80ms.

## Arquitectura
- `controllers/stream.go` recebe pedidos HTTP em `/mjpeg`
- delega a `services/mjpeg/handler.go` que faz frame-pull do socket
- frontend consome em `<img src="/mjpeg">` simples (não MediaSource)

## Histórico de investigações
- **2026-04-12:** baseline 800ms. Causa identificada: buffer do socket overflow em frames densos.
- **2026-04-20:** refactor do buffering levou a 200ms (4x). Trade-off: usa +30MB RAM.

## Hipótese actual
Compressão JPEG no encoder está a ser chamada twice — uma vez no go2rtc-side, outra no handler. Ver `mjpeg/handler.go:142`.

## Próximos passos
- [ ] Profilar `handler.go` em produção Pi 4
- [ ] Considerar memcpy zero-copy via `mmap`
```

**Convenção:** corpo segue secções `Sumário | Arquitectura | Histórico | Hipótese | Próximos passos`. Não é enforce mas é o que o LLM aprende a escrever.

## 9. Meta files por scope

```yaml
# acme-platform/.saga/meta.yml
scope: project:acme-platform
display_name: AcmePlatform — Truck Weighing Platform
inherits:
  - dept:bm
  - org:bm
sensitivity_default: internal
write_policy: requires-pr            # ou: direct
notes_dir: topics/
created_at: 2026-04-01T00:00:00Z
```

```yaml
# ~/.saga/personal/meta.yml
scope: personal
display_name: Jorge — personal layer
sensitivity_default: confidential
write_policy: direct
notes_dir: topics/
encryption:
  enabled: false                     # true quando sincronizar
  identity_path: ~/.saga/personal/.age-identity
```

## 10. Configuração do utilizador

```yaml
# ~/.saga/config.yml
user:
  email: jorge@example.com
  display_name: Jorge

layers:
  personal:
    path: ~/.saga/personal
    write: default

  dept:bm:
    path: ~/.saga/cache/dept-bm
    remote: git@github.com:Acme/saga-dept.git
    sync: pull-on-boot
    write: requires-pr

project_discovery:
  walk_up_for: .saga/meta.yml
  inherit_from_meta: true            # honra `inherits:` do project meta

retrieval:
  recall_k: 3                        # snippets injectados pelo hook
  fallback_to_body_fts: true         # se title/synonyms não bater, tenta FTS sobre body
  per_scope_max: 1                   # max 1 topic por scope no recall

embeddings:                          # Phase 1.5; ignorado em Phase 1
  provider: ollama
  model: nomic-embed-text
  endpoint: http://127.0.0.1:11434
```

## 11. Schema SQLite (índice)

SQLite é cache. Pode ser destruída e regenerada a partir dos `.md` com `saga reindex`.

```sql
-- migrations/001_init.sql

CREATE TABLE topic_index (
  id           TEXT PRIMARY KEY,
  scope        TEXT NOT NULL,
  type         TEXT NOT NULL CHECK(type IN ('profile','preference','policy','topic')),
  title        TEXT NOT NULL,
  synonyms     TEXT NOT NULL DEFAULT '[]',
  sensitivity  TEXT NOT NULL DEFAULT 'internal',
  confidence   TEXT NOT NULL DEFAULT 'proposed',
  file_path    TEXT NOT NULL,
  file_hash    TEXT NOT NULL,
  embedding    BLOB,                 -- Phase 1.5; NULL em Phase 1
  source_layer TEXT NOT NULL,
  created_at   INTEGER NOT NULL,
  updated_at   INTEGER NOT NULL,
  UNIQUE(scope, title)
) STRICT;

CREATE INDEX idx_topic_scope ON topic_index(scope);
CREATE INDEX idx_topic_type  ON topic_index(type);
CREATE INDEX idx_topic_layer ON topic_index(source_layer);
CREATE INDEX idx_topic_updated ON topic_index(updated_at DESC);

-- FTS5 sobre title + synonyms + body (fallback)
CREATE VIRTUAL TABLE topic_fts USING fts5(
  id UNINDEXED,
  scope UNINDEXED,
  title,
  synonyms,
  body,
  tokenize = 'unicode61 remove_diacritics 2'
);

-- References table — for staleness checks against git blame
CREATE TABLE topic_reference (
  topic_id    TEXT NOT NULL,
  path        TEXT NOT NULL,
  lines       TEXT,
  blame_hash  TEXT NOT NULL,
  is_stale    INTEGER NOT NULL DEFAULT 0,
  checked_at  INTEGER,
  PRIMARY KEY (topic_id, path),
  FOREIGN KEY (topic_id) REFERENCES topic_index(id) ON DELETE CASCADE
) STRICT;

CREATE INDEX idx_reference_path ON topic_reference(path);

-- Migrations table (carry over from v1)
CREATE TABLE _migrations (
  version    TEXT PRIMARY KEY,
  applied_at INTEGER NOT NULL
) STRICT;
```

## 12. Tools MCP (signatures Go)

```go
// pkg/saga/tools.go

// recall — busca topics relevantes para uma query, respeitando layers do scope actual.
type RecallArgs struct {
    Query string `json:"query"`
    K     int    `json:"k,omitempty"`     // default 3
    Scope string `json:"scope,omitempty"` // filtro opcional, e.g. "project:acme-platform"
    Type  string `json:"type,omitempty"`  // filtro opcional, e.g. "topic"
}
type RecallResult struct {
    Results []TopicSnippet `json:"results"`
}

// topic.read — lê uma topic note inteira.
type TopicReadArgs struct {
    Name  string `json:"name"`            // ex: "mjpeg-performance"
    Scope string `json:"scope,omitempty"` // disambiguator
}
type TopicReadResult struct {
    Topic    Topic            `json:"topic"`
    Stale    []StaleRef       `json:"stale,omitempty"`
}

// topic.list — lista topic notes visíveis no scope actual.
type TopicListArgs struct {
    Scope  string `json:"scope,omitempty"`
    Filter string `json:"filter,omitempty"` // glob no title
}
type TopicListResult struct {
    Topics []TopicSummary `json:"topics"`
}

// topic.write — cria ou actualiza topic note. Default scope = personal.
type TopicWriteArgs struct {
    Name       string             `json:"name"`
    Scope      string             `json:"scope,omitempty"`     // default: personal
    Title      string             `json:"title,omitempty"`
    Synonyms   []string           `json:"synonyms,omitempty"`
    Body       string             `json:"body"`
    Mode       string             `json:"mode,omitempty"`      // create|append|replace; default create-or-append
    References []TopicReference   `json:"references,omitempty"`
}
type TopicWriteResult struct {
    ID     string `json:"id"`
    Path   string `json:"path"`
    Scope  string `json:"scope"`
    Action string `json:"action"`                // created|updated|appended
}

// topic.promote — move/copia uma topic note de um scope para outro mais alto.
// Aciona git workflow apropriado (direct commit ou abre PR).
type TopicPromoteArgs struct {
    Name      string `json:"name"`
    FromScope string `json:"from_scope"`
    ToScope   string `json:"to_scope"`
    Reason    string `json:"reason,omitempty"`   // commit message
    Mode      string `json:"mode,omitempty"`     // move|copy; default copy
}
type TopicPromoteResult struct {
    Action string `json:"action"`                // committed|pr-opened
    URL    string `json:"url,omitempty"`         // PR URL se aplicável
}
```

**Tools de v1 mantidas** (`remember`, `recall` antigo) deprecadas mas funcionais durante 2 versões — escrevem em `personal/topics/scratch-<ulid>.md` para compat.

## 13. Fluxo de read (resolver + merger)

```
hook recebe stdin (Claude Code passa cwd e prompt)
  │
  ▼
resolver:
  1. cwd → walk-up → encontra acme-platform/.saga/meta.yml
  2. lê meta.inherits → carrega dept:bm, org:bm
  3. config.yml → adiciona personal (sempre)
  4. produz lista ordenada de layers activos
  ▼
reader (em paralelo, uma query SQL por layer):
  - title fuzzy match contra termos extraídos da query
  - synonym exact match
  - se nenhum match forte e fallback_to_body_fts: FTS sobre body
  - retorna topic_ids candidatos por layer
  ▼
merger (typed):
  - profile: pega só o de personal
  - preference: pega de personal, sobrepõe overrides do project
  - policy: pega o do scope mais específico
  - topic: namespaced; ordena por score; max 1 por scope (config: per_scope_max);
          total ≤ k (config: recall_k)
  ▼
formatter:
  - emite bloco <saga-context> com sections por tipo:
    <profile>, <policy>, <topic name="..." scope="...">
  - cada topic inclui referências de ficheiros (para a IA não re-procurar)
  - flags ⚠ stale se topic_reference.is_stale para alguma ref
  ▼
stdout → Claude Code prepende ao prompt
```

## 14. Fluxo de write (default-personal, promote-on-confirm)

```
IA invoca topic.write(name="mjpeg-performance", body="...")
  │
  ▼
1. Scope = personal (default; IA não tem que pensar)
2. Carrega/cria ficheiro: ~/.saga/personal/topics/mjpeg-performance.md
3. Frontmatter:
   - se não existe: gera id (ULID), created_at, created_by
   - se existe: actualiza updated_at, updated_by
4. Mode:
   - create: erro se já existe
   - append: adiciona secção "## Update YYYY-MM-DD" no fim
   - replace: substitui body inteiro
5. Reescreve ficheiro (atomic write: tmp + rename)
6. git add + git commit -m "saga: <action> <scope>:<name>"
7. Reindex SQLite para esta nota (parse frontmatter + body)
8. Retorna { id, path, scope, action }
```

**Promoção** (separadamente, via `topic.promote`):

```
IA invoca topic.promote(name="mjpeg-performance",
                        from="personal", to="project:acme-platform",
                        reason="validated by 3 successful debugs")
  │
  ▼
1. Lê meta.yml do target layer → write_policy
2. Se "direct":
   - copy/move ficheiro para acme-platform/.saga/topics/mjpeg-performance.md
   - actualiza scope no frontmatter
   - git add/commit/push no repo do projeto
3. Se "requires-pr":
   - cria branch saga/promote-<name>-<short-sha>
   - copy/move ficheiro
   - commit + push
   - invoca `gh pr create` com title/body sensatos
   - retorna URL do PR para a IA mostrar ao user
4. Se mode=copy: original permanece em personal (com tag stale opcional)
   Se mode=move: remove de personal
5. Reindex SQLite
```

## 15. Stale invalidation

Cada `topic_reference` guarda `blame_hash` (sha curto do `git blame` na linha referenciada, no momento da escrita).

`saga lint --stale` (e idealmente um pre-recall opcional):

```
1. Para cada topic_reference, executa: git blame -L <lines> <path> -- HEAD
2. Compara hash actual vs stored blame_hash
3. Se diferente: set is_stale=1, checked_at=now
4. Output: lista de notas com refs stale
```

Recall sinaliza topics com refs stale em vez de os esconder. A IA é instruída a *propor refresh* dessas notas em vez de confiar nelas cegamente.

## 16. Segurança

| Vector | Defesa | Onde |
|---|---|---|
| Repo do projeto leak | Notas nunca contêm secrets (convenção). Lint opcional anti-entropy. | Convenção + `saga lint --secrets` |
| Personal layer no FS | `chmod 0700 ~/.saga/personal/` na inicialização | Kernel |
| Personal sync para remote | Encryption obrigatória com `age`, chave em OS keychain | Kernel (Phase 2) |
| Sensitivity awareness | Frontmatter `sensitivity:` linta promote para scopes inadequados | Kernel |
| Promote acidental | Auto-promote = proibido. AI propõe, humano confirma. | Convenção |
| Multi-user dev server | Filesystem perms (0700 home) | OS |
| AI alucina e sobrepõe nota correcta | Tudo é git commit, revertível | Substrato |
| Atacante ganha shell | Mesmo problema que ter código no disco; fora do âmbito da Saga | Não defendido |

**Princípio:** Saga não inventa AuthN/AuthZ. Herda de git e do OS. Adiciona apenas crypto opt-in para personal layer e linting de promoção.

## 17. Roadmap revisto

> Phases não são waterfall. Phase 1 tem que estar em uso real 4 semanas antes de qualquer outra disparar.

### Phase 0 ✅ (closed)
Spec, decisões iniciais, scaffolding TypeScript.

### Phase 1 — Loop mínimo em Go (NOVA, substitui Phase 1 v1)

**Resolve:** prova que topic-grained RAG com layered scopes reduz repetição em uso diário em ≥3 projectos.

**Deliverables:**
- Reescrita Go: `saga-mcp`, `saga-hook`, `saga` CLI básica.
- Layers: apenas `personal` (init automático como git repo) e `project` (discovery por walk-up).
- Tipos: `profile` e `topic` (deixa `preference`/`policy` para Phase 2).
- Tools MCP: `recall`, `topic.read`, `topic.list`, `topic.write`. (Sem `promote` ainda.)
- Hook que injecta com cwd-awareness.
- `saga reindex` (rebuild SQLite a partir dos `.md`).
- Tests: parser de frontmatter, resolver de layers, merger por tipo, FTS query sanitization (corrigida do bug de v1).

**Done quando:**
- 4 semanas de uso diário em ≥3 projectos teus, em ≥2 computadores.
- ≥10 topic notes maduras em pelo menos 2 projectos.
- Métrica: contas a sentir-te a repetir menos. (Subjectivo intencional; instrumentação numérica vem em Phase 1.5.)

### Phase 1.5 — Embeddings (quando o keyword falhar)
- `sqlite-vec` extension carregada lazy.
- `OllamaProvider` (default) gera embeddings de notas existentes.
- Recall passa a híbrido (BM25 sobre title/synonyms; cosine sobre body como reranker).
- Métrica instrumentada: `recall_event` table (queries, hits, hits-clicked-by-AI).

**Trigger:** ≥3 falhas documentadas onde a nota relevante existia mas não foi devolvida por mismatch keyword.

### Phase 2 — Department layer + promotion
- Suporte a `dept:` e `org:` layers via clone+pull.
- `topic.promote` com workflow git (direct ou requires-pr).
- `preference` e `policy` types com merge typed.
- Encryption do personal layer com `age` quando sincronizado.
- Sensitivity linting em promote.

### Phase 3 — Stale invalidation
- `topic_reference` populada em cada write com `git blame` hash.
- `saga lint --stale` em CI ou pre-recall.
- Recall flag ⚠ stale.

### Phase 4 — Multi-vendor backfill
- Adapters para extrair notes de transcripts existentes (Claude Code, Cursor, ChatGPT export).
- Tool `consolidate` invoca LLM para propor topic notes a partir de N transcripts.
- Notas geradas entram como `confidence: proposed` em personal layer; review humano antes de promover.

### Phase 5+ — Acessórios opcionais (sem compromisso)
- Web dashboard read-only sobre os ficheiros markdown.
- Saga-Gateway: produto separado com endpoint runtime no projeto, MCP composável.
- Cross-project knowledge graph (org-level analytics).

## 18. Decisões registadas

| Decisão | Estado | Notas |
|---|---|---|
| Linguagem Go | LOCKED | Substitui TypeScript de v1. Re-escrita ~3-5 dias. |
| SQLite + markdown (ficheiros = fonte) | LOCKED | Substitui tabela única de v1. |
| Layered scopes | LOCKED | Personal/Project obrigatórios em Phase 1; Dept/Org em Phase 2. |
| Tipologia profile/preference/policy/topic | LOCKED | Em Phase 1 só profile e topic. |
| Default-personal write, explicit promote | LOCKED | Privacidade por default. |
| sqlite-vec + Ollama em Phase 1.5 | LOCKED | Coluna `embedding` reservada desde Phase 1. |
| Substrato git, sem sync inventado | LOCKED | Sync = git pull/push. |
| Sem Auth próprio | LOCKED | Herda de git ACL + OS perms. |
| Single-tenant ("uma mente, um MCP") | LOCKED | Mantido de v1. Cada utilizador corre o seu binário. |

## 19. Não-objectivos explícitos

Para evitar scope creep — Saga **não faz** nem fará:

- Servidor central (cloud SaaS).
- Multi-tenant no mesmo binário.
- Real-time sync (CRDT, OT).
- Runtime gateway ao projeto vivo (telemetria, queries operacionais) — fica para `saga-gateway`, produto separado.
- Dashboards interactivos no kernel — ficam para `saga-ui`, separado.
- Auth/RBAC próprio.
- LLM próprio. A inteligência vem dos clientes MCP.

A ausência destes itens não é esquecimento — é disciplina.

## 20. Divergências face ao COGNITIVE_MODEL (a alinhar pós-Iter 1 e 2)

Este documento (DESIGN_v2) foi escrito antes do `COGNITIVE_MODEL.md`. Os pontos abaixo identificam onde precisa de update — não os corrige aqui (evitar churn em doc grande); cada um vai ser resolvido no commit que implementa a feature correspondente.

### §6 Tipologia — re-equilibrar peso entre tipos

A tabela actual lista `profile | preference | policy | topic` com 4 linhas equivalentes. **A realidade pós-pivot é que `profile` é coluna vertebral**, não tipo entre quatro. Update após Iter 1: nota explícita que `profile` (+ `preference`) é injectado *sempre* via baseline; `policy`/`topic` injectados *condicionalmente* via match. Adicionar tipo `skill` como Iter 6 (conditional, via T3.3).

### §11 Schema SQLite — falta tabela `lembranca`

O schema actual cobre `topic_index` + `topic_fts` + `topic_reference`. Falta `lembranca` (camada episódica L2). Adicionada na migration `002_lembrancas.sql` em Iter 2. Update do §11: incluir tabela completa, índices, FK em cascade para `topic_index`.

### §13 Fluxo de read — não menciona baseline always-on

O diagrama actual descreve resolver → reader → merger → formatter, mas o reader busca apenas tópicos que batem na query. **Falta o passo `BuildIdentityBaseline` que injecta sempre profile + preferences**, independente da query. Update após Iter 1: separar fluxo em 2 andares — *(a) baseline build (always)*, *(b) topic match (query-relevant)*.

### §15 Stale invalidation — alinhar com lembranças

A definição de "stale" actual é via `git blame` hash. **Adicionar segunda dimensão: notas com `last_lembrança_at` muito antigo (≥6 meses) são candidatas a archive**, mesmo que o git blame ainda bata. Update após Iter 2 + 4+: campo derivado `last_lembrança_at` no `topic_index` (computado de `lembranca`).

### §17 Roadmap — substituído por PLAN.md + ROADMAP_v2.md

A secção §17 lista Phases 1, 1.5, 2, 3, 4, 5+. **Está desactualizada** — `PLAN.md` e `ROADMAP_v2.md` são agora autoritários, com iterações alinhadas ao modelo cognitivo. Update: substituir §17 por uma só linha apontando para `PLAN.md`/`ROADMAP_v2.md`. Manter §17 actual num apêndice como "v1 roadmap, histórico".

### Linguagem geral — "memory" vs "lens"

Várias referências (README, MCP tool descriptions, §1 visão) falam de Saga como "memory layer". O modelo cognitivo prefere "lens" — *Saga é a lente que torna a IA tua, não memória que ela consulta*. Update transversal após Iter 1: substituir "memory" por "lens" em strings user-facing.

---

**Política:** estas divergências são **conhecidas e aceites**. Não bloqueiam Iters 1 e 2. Cada uma é resolvida no commit da iteração correspondente, com referência explícita a este §20.
