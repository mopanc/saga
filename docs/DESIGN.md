# Saga — Design

## 1. Visão

Repetimo-nos a cada IA. Cada conversa começa do zero. Saga é uma camada de memória local-first que vive entre nós e as IAs: capturamos uma vez, qualquer IA recupera quando precisa, sem nos perguntar.

## 2. Princípios de design (LOCKED)

1. **Invisibilidade.** Sucesso = melhorar o raciocínio sem exigir gestão activa. Se tiveres de pensar no sistema para funcionar, falhou.
2. **Validação por uso, não por features.** Cada fase só avança com evidência da fase anterior. Sem evidência, sem fase nova.
3. **Taxonomia emerge dos dados.** Começamos com texto raw + tags livres. A estrutura aparece quando os padrões aparecem.
4. **Local-first.** SQLite no disco. Cloud é opt-in, nunca default.
5. **Um core, vários frontends.** A lógica vive numa lib única. MCP, REST, CLI são entrypoints.
6. **Soberania.** Export portável JSON sempre disponível. Os dados são teus.
7. **Datas auditam, não decidem.** Contradição resolve-se por dimensão semântica e estabilidade — datas são metadado.

## 3. Métrica de sucesso

A única métrica que importa:

> Quantas vezes esta semana o sistema melhorou a conversa **sem eu pensar nele**.

Medições derivadas (semana a semana):

- Repetições de contexto a IAs (deve cair).
- Surpresas positivas — IA sabe algo que não foi dito nesta sessão.
- Memórias retornadas que foram lixo (deve cair).
- Memórias nunca retornadas em 4+ semanas (sinal de ruído — corte).
- Capturas que faltaram (sinal de capture surface insuficiente).

## 4. Arquitectura

```
                ┌─────────────────────────────────────┐
                │             core (lib)              │
                │  - SQLite storage + sqlite-vec      │
                │  - capture / recall / retrieval     │
                │  - extracção / consolidação         │
                │  - export / import                  │
                └─────────────┬───────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
   ┌────▼────┐         ┌──────▼──────┐       ┌──────▼──────┐
   │   MCP   │         │  HTTP REST  │       │     CLI     │
   │ server  │         │ (127.0.0.1) │       │             │
   └─────────┘         └─────────────┘       └─────────────┘
        │                     │                     │
        ▼                     ▼                     ▼
  Claude Code           Dashboard,            Revisão manual,
  Cursor                mobile,               queries rápidas,
  outras (MCP)          scripts cron          export
```

**Fluxo de dados:**

1. **Captura** — IA chama `remember(...)`, ou batch import, ou CLI manual.
2. **Storage** — SQLite + FTS5 (Fase 1). `sqlite-vec` para embeddings reservado para Fase 1.5.
3. **Retrieval** — `recall(query)` BM25 (Fase 1); híbrido com cosine em Fase 1.5.
4. **Injecção** — hook `UserPromptSubmit` injecta top-k snippets antes do prompt.

A IA nunca tem de pedir contexto. O sistema serve antes dela perguntar.

## 5. Stack & estrutura

**Stack (LOCKED):**

- **Linguagem:** TypeScript — ecossistema MCP nativo (`@modelcontextprotocol/sdk`), Zod para schemas partilhados, frontends fáceis (Hono para HTTP, commander para CLI).
- **Storage:** SQLite + FTS5 nativo (Fase 1). `sqlite-vec` instalável em Fase 1.5 sem migração disruptiva — coluna `embedding` já reservada.
- **Embeddings:** adiados para Fase 1.5. Quando chegarem, via interface `EmbeddingProvider` em `core` — implementações swappable: **Ollama** (`nomic-embed-text`, default soberano) ou **API externa** (Voyage `voyage-3-lite`, OpenAI `text-embedding-3-small`). Selecção em config, não em código.
- **Build/run:** `tsx` ou `bun` em dev; bundle por `tsup` para distribuição.

**Layout do monorepo:**

```
saga/
├── README.md
├── docs/
│   ├── DESIGN.md
│   └── ROADMAP.md
├── packages/
│   ├── core/          # lógica + DB + retrieval (lib)
│   ├── mcp/           # MCP server (binário)
│   ├── api/           # HTTP REST (binário)
│   └── cli/           # CLI (binário)
├── migrations/        # schema versioning
├── tests/             # core tem prioridade absoluta
└── scripts/           # setup, backup, import adapters
```

**Princípios estruturais:**

- `core` não importa de nenhum frontend. Frontends importam só de `core`.
- Tipos partilhados (Zod schemas) vivem em `core`.
- Migrations versionadas desde o commit 1, irrevogavelmente.
- Tests no core são obrigatórios — é onde se ganha ou perde a confiança no sistema.

## 6. Modelo de dados — Fase 1 (LOCKED)

Uma única tabela. Sem taxonomia, sem dimensões, sem stability.

```sql
CREATE TABLE memory (
  id          TEXT PRIMARY KEY,           -- ulid
  text        TEXT NOT NULL,              -- conteúdo livre
  tags        TEXT NOT NULL DEFAULT '[]', -- JSON array, livres
  embedding   BLOB,                       -- vector via sqlite-vec
  source      TEXT,                       -- origem: claude-code, cli, ...
  session_id  TEXT,                       -- sessão de captura (proveniência mínima)
  created_at  INTEGER NOT NULL            -- unix ms
);

CREATE INDEX idx_memory_created ON memory(created_at DESC);

-- + FTS5 virtual table para keyword search (Fase 1)
-- coluna `embedding` reservada (NULL em Fase 1)
-- sqlite-vec virtual table criada em Fase 1.5; retrieval passa a híbrido sem mudar a API
```

**Justificação:** capturar sinal antes de impor estrutura. A taxonomia (Fase 2+) será construída em cima desta tabela, não em vez dela.

## 7. Modelo de dados — Fase 3+ (PROPOSED, evidence-pending)

A propor **só após** dados de Fase 1-2 confirmarem necessidade. Capturado aqui para não perder o raciocínio:

- **4 camadas:** identidade, preferências, estado, conhecimento (+ episódico).
- **Tipos candidatos:** `trait`, `value`, `skill`, `preference`, `taste`, `rule`, `task`, `project_state`, `focus`, `fact`, `note`, `decision`, `episode`.
- **Campos de resolução de contradição:**
  - `dimension` — chave semântica onde a contradição é detectável.
  - `scope` — contexto onde aplica (preferências contraditórias com scope disjunto **não são contradição**).
  - `stability` — `core | slow | fast | volatile`. Decide se conflito auto-resolve ou bloqueia.
  - `state` — `active | superseded | retired | proposed`.
- **Provenance forte:** `tool, session_id, raw_episode_id, recorded_at`. Permite revogação em massa por sessão.
- **Confidence + review_status:** factos auto-extraídos não são gospel; entram como `proposed` e vão para fila de revisão.

> Crítico: isto não é spec. É **hipótese a validar** contra os dados que a Fase 1 vai gerar. Possivelmente metade desta secção morre.

## 8. Tools MCP — Fase 1 (LOCKED)

### `remember(text, tags?, source?)`

Captura uma memória. Retorna `id`.

- `text: string` — conteúdo livre.
- `tags?: string[]` — etiquetas livres, opcionais.
- `source?: string` — default ao client (ex: `"claude-code"`).

### `recall(query, k?)`

Retrieval BM25 (FTS5) — Fase 1. Retorna lista de snippets com score.

- `query: string`
- `k?: number` — default 5.

> Em Fase 1.5 a implementação interna troca para híbrido (BM25 + cosine) sem mudar a assinatura da tool. Os clientes MCP não notam.

**Sem mais tools nesta fase. Resistir a adicionar.**

## 9. Hook de injecção — Fase 1

Hook `UserPromptSubmit` no Claude Code:

1. Recebe o prompt do user.
2. Chama `recall(prompt, k=3)`.
3. Pré-pende ao prompt um bloco com os snippets retornados (com origem visível).
4. Submete ao LLM.

## 9.5. Sobre IA — o que a Saga usa e o que não usa

Pergunta legítima: *"se já uso o Claude Code pago, vou ter de configurar outra IA?"*

**A Saga não tem LLM próprio.** É uma base de dados com tools. O Claude Code (ou qualquer outro cliente MCP) é a inteligência — chama as tools quando precisa. Zero IA dentro da Saga.

**A excepção são os embeddings** — números que representam o significado de cada memória, usados em retrieval semântico. O Claude Code não expõe API de embeddings sobre os teus dados, logo embeddings precisam de um modelo separado. Por isso:

- **Fase 1:** sem embeddings. Retrieval por keyword (BM25 via FTS5) é suficiente para validar o loop. Zero deps externas, zero chave, zero Ollama.
- **Fase 1.5:** quando o keyword falhar, ligas embeddings via `EmbeddingProvider`:
  - **Ollama local** (default) — `nomic-embed-text`, corre na tua máquina. Zero custo, zero chave.
  - **API externa** (configurável) — Voyage ou OpenAI. Cêntimos/mês para uso pessoal.
- **Fase 4 (`consolidate_episodes`):** *essa* fase precisa de um LLM (extrair factos de transcripts). Opções na altura: Anthropic API directa, ou chamar `claude` CLI como subprocess (já o pagas). Decisão adiada.

**Resumo:** a Saga é gratuita e sem dependências de IA externas até à Fase 1.5. E mesmo aí, Ollama local é suficiente — API externa é opcional.

## 10. Decisões registadas

| Decisão | Estado | Notas |
| --- | --- | --- |
| Local-first, SQLite | LOCKED | Não-negociável. |
| Stack TypeScript | LOCKED | Ecossistema MCP nativo, Zod, Hono. |
| Single-tenant | LOCKED | *"Uma mente, um MCP"* — multi-tenant não faz sentido conceptual. |
| Taxonomia emerge dos dados | LOCKED | Sem 12 tipos pré-impostos. |
| Datas não resolvem contradições | LOCKED | Audit, não decisão. |
| Validation-first, fase a fase | LOCKED | Sem evidência, sem fase nova. |
| BM25-only em Fase 1, embeddings em Fase 1.5 | LOCKED | Coluna `embedding` reservada desde Fase 1. Sem deps externas no arranque. |
| `EmbeddingProvider` swappable (Ollama default, API externa configurável) | LOCKED | Selecção via env var. Ollama recomendado para soberania. |
| Provenance forte | PLANNED Fase 2 | Adiado mas registado. |
| Confidence + review queue | PLANNED Fase 2+ | Quando consolidação automática começar. |

## 11. Perguntas em aberto

1. **Backfill prioridade Fase 4:** transcripts antigos do Claude Code primeiro? ChatGPT exports? — decisão adiada para Fase 4.
