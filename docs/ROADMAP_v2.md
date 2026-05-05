# Saga — Roadmap (v2)

> Plano granular de execução. Cada task tem ficheiro(s) afectado(s), critério de aceitação, dependências e esforço estimado. Substitui o `ROADMAP.md` v1 (histórico).
>
> Para o "porquê" e modelo cognitivo: ver `COGNITIVE_MODEL.md`. Para iterações de alto nível e testes de utilidade: ver `PLAN.md`.

## Convenções

- **Branch de trabalho:** `pivot/v2-go` (até v2 estabilizar e fazer merge a `main`)
- **Commits:** Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`)
- **Tests:** todo o código novo em `internal/saga/` ou `internal/mcp/` traz teste. CLI cmd não precisa.
- **Build/test antes de commit:** `go build ./... && go test ./...`
- **PR:** não obrigatório enquanto solo dev em `pivot/v2-go`. Será obrigatório quando merge para `main`.

---

## Iteration 0 — Ground truth

**Objectivo:** colocar o esqueleto cognitivo em uso real, com a tua identidade dentro.

### T0.1 — Profile note (identidade)

- **Ficheiro:** `~/.saga/personal/profile/identity.md`
- **Descrição:** Markdown com frontmatter completo + body estruturado em secções: *Identity, Languages, Career arc, Capabilities, Domain expertise, Current focus, Working style, Stack preferences, Public surfaces.*
- **Frontmatter mínimo:** `id`, `scope: personal`, `type: profile`, `title: "Jorge Morais — identity"`, `sensitivity: confidential`, `confidence: validated`, `created_at`, `updated_at`.
- **Body:** ≥500 palavras, factual, em PT-PT.
- **Acceptance:** depois de `saga reindex`, `saga` indexa-a sem erros e `recall` com query `"jorge"` devolve-a.
- **Dependências:** nenhuma.
- **Esforço:** 1h.

### T0.2 — Preferências de comunicação

- **Ficheiro:** `~/.saga/personal/preferences/communication.md`
- **Frontmatter:** `type: preference`.
- **Body:** lista explícita: *idioma (PT-PT default), tom (directo, sem sycophancy), comprimento de resposta, quando pedir clarificação vs avançar, formato preferido (markdown, tabelas), uso de emojis (não), uso de filler ("certo!", "claro!"), …*
- **Acceptance:** indexada; `recall "comunicação"` ou `"tom"` devolve-a.
- **Dependências:** nenhuma.
- **Esforço:** 15m.

### T0.3 — Policy de código

- **Ficheiro:** `~/.saga/personal/policy/code-style.md`
- **Frontmatter:** `type: policy`.
- **Body:** estilo de commits (Conventional?), estratégia de branching, código preferido (verbose vs idiomatic, comments, error handling), preferência de testes, padrões de PR, ferramentas (preferred linters/formatters por linguagem).
- **Acceptance:** indexada; `recall "commit"` ou `"branching"` devolve-a.
- **Dependências:** nenhuma.
- **Esforço:** 30m.

### T0.4 — Anotação de divergências em DESIGN_v2.md

- **Ficheiro:** `docs/DESIGN_v2.md` (edição)
- **Descrição:** adicionar uma secção curta no fim — *"§20 — Divergências face ao COGNITIVE_MODEL"* — listando explicitamente onde o doc precisa de update após Iteration 1 (ex: §13 read flow não menciona baseline always-on, §17 Phase 1 não inclui lembranças, etc).
- **Sem reescrever o doc agora**, só anotar pendências.
- **Acceptance:** secção §20 existe e referencia ≥3 pontos a alinhar.
- **Dependências:** nenhuma.
- **Esforço:** 30m.

### T0.5 — Smoke do estado pós-Iteration 0

- **Acção:** correr `saga reindex` na home, depois cd para um projecto teu, abrir Claude Code, fazer pergunta sobre algo do teu profile.
- **Acceptance:** a IA responde calibrada por ti — refere-se ao teu papel, idioma, área, sem teres explicado nada nesta sessão.
- **Dependências:** T0.1, T0.2, T0.3.
- **Esforço:** 15m.

**Saída da Iteration 0:** profile populado, conhecimento explícito sobre ti capturado, divergências cognitivas anotadas no design. Pronto para Iteration 1 que torna o hook *sempre-ligado*.

---

## Iteration 1 — Lente sempre-ligada (F3 real)

**Objectivo:** o hook deixa de fazer só search-by-query; passa a injectar **sempre** o baseline de identidade + tópicos relevantes quando aplicável.

### T1.1 — `BuildIdentityBaseline()` em `internal/saga/baseline.go`

- **Ficheiro:** `internal/saga/baseline.go` (novo)
- **Função:** `BuildIdentityBaseline(svc *Service, maxTokens int) (string, error)`
- **Comportamento:**
  - Lê todas as notas de tipo `profile` da layer `personal`
  - Lê todas as notas de tipo `preference` da layer `personal`
  - Sintetiza num bloco compacto markdown: cabeçalho de identidade (nome, idiomas, papéis), preferências essenciais (tom, idioma, formato)
  - Limita a `maxTokens` (estimativa simples: 4 chars ≈ 1 token; trunca em fronteira de secção)
  - Retorna string vazia se profile vazio (não inventa nada)
- **Acceptance:**
  - Determinístico (mesmo input → mesmo output)
  - Respeita o limite de tokens
  - Tem teste em `baseline_test.go` com profile fake
- **Dependências:** Iteration 0 completa.
- **Esforço:** 2h.

### T1.2 — Refactor de `cmd_hook.go` para output em duas secções

- **Ficheiro:** `cmd/saga/cmd_hook.go`
- **Changes:** `emitContext()` torna-se `emitContext(w, baseline, results)`; output é:
  ```
  <saga-identity>
  …baseline…
  </saga-identity>

  <saga-context>
    <topic …>…</topic>
    …
  </saga-context>
  ```
- **Comportamento:** `<saga-identity>` é emitido **sempre** (mesmo com 0 results); `<saga-context>` só se há results.
- **Acceptance:**
  - Smoke test e2e: hook recebe stdin com prompt → stdout tem `<saga-identity>` sempre.
  - Latência adicional do hook < 50ms numa máquina típica (medir com `time`).
- **Dependências:** T1.1.
- **Esforço:** 1h.

### T1.3 — Configurabilidade do limite de baseline

- **Ficheiros:** `internal/saga/config.go`, `internal/saga/baseline.go`
- **Changes:** novo campo `BaselineMaxTokens int` em `Config`, default 400; lido de env var `SAGA_BASELINE_MAX_TOKENS`.
- **Acceptance:** override via env var funciona; default respeitado se não definido.
- **Dependências:** T1.1.
- **Esforço:** 30m.

### T1.4 — Tests

- **Ficheiros:** `internal/saga/baseline_test.go` (novo); update em `cmd_hook` (mas é cmd, então e2e via shell smoke).
- **Casos:**
  - Profile vazio → baseline vazio
  - Profile + preference → baseline contém ambos resumidos
  - Profile longo → respeita limite de tokens (truncado em fronteira limpa)
  - Múltiplas notas de profile → todas consideradas
- **Acceptance:** ≥4 testes, todos a passar.
- **Dependências:** T1.1.
- **Esforço:** 1h.

### T1.5 — Update das descriptions dos tools MCP

- **Ficheiro:** `cmd/saga/cmd_mcp.go`
- **Changes:** descrição da tool `recall` deixa claro que o hook já injecta baseline automaticamente — IAs devem chamar `recall` para casos *específicos*, não para descobrir identidade do user (essa vem grátis).
- **Acceptance:** lendo a description, fica claro o split entre baseline (sempre, automático) e `recall` (sob demanda).
- **Dependências:** T1.1, T1.2.
- **Esforço:** 30m.

### T1.6 — Smoke pós-Iteration 1

- **Acção:** instala binário novo, refaz `setup-claude` se necessário, reinicia Claude Code, testa em ≥3 projectos diferentes durante 3 dias.
- **Acceptance:** *"sente-se que ele me conhece"* validado em ≥10 sessões consecutivas; latência percepcionada do prompt sem regressão notável.
- **Dependências:** T1.1–T1.5.
- **Esforço:** 30m de instalação + 3 dias de uso real.

**Saída da Iteration 1:** F3 real. Cada IA configurada arranca cada prompt sabendo quem és.

---

## Iteration 2 — Lembranças (camada episódica)

**Objectivo:** sistema com história de pensamento — cada injecção/recall regista evento.

### T2.1 — Migration `002_lembrancas.sql`

- **Ficheiro:** `internal/saga/migrations/002_lembrancas.sql`
- **Schema:**
  ```sql
  CREATE TABLE lembranca (
    id           TEXT PRIMARY KEY,            -- ULID
    topic_id     TEXT NOT NULL,
    triggered_at INTEGER NOT NULL,            -- unix ms
    kind         TEXT NOT NULL CHECK(kind IN ('hook','recall','topic_read','baseline')),
    query        TEXT,                         -- texto da query, NULL para baseline
    cwd          TEXT,                         -- contexto onde aconteceu
    was_used     INTEGER,                      -- 0/1, NULL se não há feedback
    outcome      TEXT,                         -- helpful|irrelevant|wrong|NULL
    FOREIGN KEY (topic_id) REFERENCES topic_index(id) ON DELETE CASCADE
  ) STRICT;

  CREATE INDEX idx_lembranca_triggered ON lembranca(triggered_at DESC);
  CREATE INDEX idx_lembranca_topic     ON lembranca(topic_id);
  CREATE INDEX idx_lembranca_kind      ON lembranca(kind);
  ```
- **Acceptance:** migração aplicada idempotente; teste em `db_test.go` confirma tabela existe e é vazia em DB nova.
- **Dependências:** nenhuma (extensão do schema).
- **Esforço:** 1h.

### T2.2 — Logging automático de lembranças

- **Ficheiros:** `internal/saga/service.go`, `internal/saga/baseline.go`, `cmd/saga/cmd_hook.go`
- **Changes:**
  - Novo método `(*DB).LogLembranca(topicID, kind, query, cwd string)`
  - `Service.Recall()` invoca-o para cada result devolvido (kind=`recall`)
  - `Service.TopicRead()` invoca-o (kind=`topic_read`)
  - Hook invoca-o por cada topic injected (kind=`hook`) e uma vez por baseline emit (kind=`baseline`)
- **Acceptance:** após uma sessão de teste, `SELECT COUNT(*) FROM lembranca` > 0; cada kind aparece pelo menos uma vez.
- **Dependências:** T2.1.
- **Esforço:** 2h.

### T2.3 — Tool MCP `lembranca_log`

- **Ficheiro:** `cmd/saga/cmd_mcp.go`
- **Schema:** parâmetros `since` (ms ou ISO date), `kind` (filtro opcional), `limit` (default 50).
- **Output:** lista de lembranças com timestamp, kind, topic title, query.
- **Acceptance:** chamada via MCP devolve dados; teste em service_test.
- **Dependências:** T2.1, T2.2.
- **Esforço:** 1h.

### T2.4 — Recency factor no ranking de `recall`

- **Ficheiro:** `internal/saga/service.go`
- **Changes:** scorer passa de `score = -bm25` para `score = -bm25 + recency_weight(latest_lembrança_at)`. `recency_weight` decai exponencialmente — peso 1.0 se < 1h, 0.5 se < 1 dia, 0.1 se > 7 dias, 0 se nunca.
- **Acceptance:** notas com lembranças recentes aparecem mais cedo; teste com fixtures cobre 3 cenários.
- **Dependências:** T2.1, T2.2.
- **Esforço:** 1h.

### T2.5 — CLI `saga lembrancas`

- **Ficheiro:** `cmd/saga/cmd_lembrancas.go` (novo)
- **Função:** lista as últimas N lembranças com filtros (--since, --kind, --topic).
- **Output:** tabela human-readable.
- **Acceptance:** comando funciona em terminal; smoke em script.
- **Dependências:** T2.1, T2.2.
- **Esforço:** 30m.

### T2.6 — Tests

- **Ficheiros:** `internal/saga/db_test.go` (update), `internal/saga/service_test.go` (update)
- **Casos:**
  - Migration aplicada cria tabela lembranca
  - `LogLembranca()` insere row corretamente
  - `Recall()` cria lembranças
  - Recency weight aplicado no scoring
  - Cascade delete: deletar topic deleta as lembranças associadas
- **Acceptance:** ≥6 testes a passar.
- **Dependências:** T2.1–T2.4.
- **Esforço:** 1h.

**Saída da Iteration 2:** sistema com história de pensamento; ranking influenciado por uso real; possibilidade de análise (`que tópicos foram usados esta semana?`).

---

## Iteration 3 — Validação prolongada (sem código)

**Objectivo:** detectar o que falta empiricamente em vez de adivinhar.

### T3.1 — Diário de uso (manual)

- **Ficheiro:** `~/.saga/personal/topics/saga-usage-week-N.md` (uma nota por semana)
- **Conteúdo:** observações livres: o que foi útil, o que falhou, lembranças que deviam ter aparecido mas não apareceram, atrito sentido, surpresas positivas.
- **Acceptance:** 4 semanas de notas, uma por semana.
- **Dependências:** Iterations 1 e 2 deployed.
- **Esforço:** 15m/semana.

### T3.2 — Métricas baseadas em lembranças

- **Acção:** correr semanalmente `saga lembrancas --since 7d` e analisar:
  - Total de lembranças
  - Distribuição por kind
  - Top tópicos lembrados
  - Tópicos zero-lembrança (candidatos a archive ou irrelevantes)
- **Acceptance:** insight semanal documentado no diário de uso.
- **Dependências:** Iteration 2 deployed, T3.1.
- **Esforço:** 15m/semana.

### T3.3 — Decisão Iteration 4+

- **Acção:** ao fim de 4 semanas, escrever em `docs/notes/iteration-3-conclusions.md` o que sai da observação. Decidir qual de [valência, sessão, skill, promote, stale, vector] é a próxima iteração com base em pain real.
- **Acceptance:** doc com decisão argumentada e plano de ataque.
- **Dependências:** T3.1, T3.2 (4 semanas de dados).
- **Esforço:** 1h.

**Saída da Iteration 3:** roadmap de Iteration 4+ priorizado por evidência.

---

## Iteration 4+ — Conditional, ordem decidida em T3.3

Cada candidato fica aqui em pré-formato — só se executa se T3.3 a priorizar:

### Iter 4 (provável) — Valência

- Adicionar tool MCP `lembranca_mark(id, outcome)` para feedback humano
- Tool MCP `feedback` para a IA marcar quando achou útil/inútil pós-uso
- Scorer integra valência: `score = bm25 + recency + valence_weight`
- Tests
- **Trigger:** ranking falhou consistentemente (≥30% rejeições) em T3.

### Iter 5 — Sessão (working memory)

- Cache em memória de actividade da última hora (cwd, recent files, recent queries)
- Pesa ranking com factor de "thematic continuity"
- **Trigger:** sentes que sistema "esquece" o que fizeste há 30min em T3.

### Iter 6 — Tipo `skill` distinto de `policy`

- Adicionar `skill` à enum em `topic_index.type`
- Schema diferenciado (skill = template + exemplo; policy = regra)
- Tests
- **Trigger:** ≥5 patterns "como faço X" capturados em T3.

### Iter 7 — `topic.promote` real com PR

- Implementar promotion workflow: copy/move entre layers
- Integration com `gh pr create` quando `write_policy: requires-pr`
- Tests
- **Trigger:** começas a usar dept layer ou partilhar com colegas em T3.

### Iter 8 — Stale invalidation com `git blame`

- `saga lint --stale` percorre `topic_reference`, compara `blame_hash` com actual
- Marca `is_stale=1`
- Recall flag stale notes
- **Trigger:** notas começam a referenciar código que mudou em T3.

### Iter 9 — Embeddings (sqlite-vec + Ollama)

- Carregar extension `sqlite-vec`
- Migration para virtual table `topic_vec`
- `OllamaProvider` em `internal/saga/embedding.go`
- Recall híbrido (BM25 + cosine)
- Backfill em batch para notas existentes
- **Trigger:** ≥3 falhas documentadas onde memória existe mas BM25 não devolveu (lexical mismatch).

---

## Iteration ∞ — Sem prioridade, casos de uso futuros

Sem trigger automático; entram só com demanda concreta:

- **`saga-ui`** — dashboard read-only sobre os ficheiros markdown. Visualização do grafo de lembranças, listing por scope, etc.
- **`saga-gateway`** — produto separado, runtime queries ao projecto vivo (telemetria, configs). Compõe via MCP, não vive dentro do core.
- **Importers** — adapters para transcripts (Claude Code session logs, Cursor, ChatGPT export) com `saga import`. LLM consolida em topic notes propostas.
- **Encryption do personal layer** — `age`-based, opt-in para sync seguro.
- **Web auth / multi-user** — só faz sentido se mudarmos de "single mind" para "team space" (mudança de produto).

---

## Tracking de progresso

Marca tasks completadas inline (`✅`) ou via commit message convention `roadmap: T0.1 done`. Nada de tooling especializado para já — git log + este doc bastam.

---

## Triggers explícitos para mudar este roadmap

- **Iteration 0–2 demonstrou alguma assumpção errada do design** → refactor antes de continuar; doc em `docs/notes/` justifica.
- **Iteration 3 mostrou pain inesperado** → reordenar Iter 4+ accordingly; T3.3 documenta.
- **Anthropic/OpenAI lançam memory feature obviadora** → repensar posicionamento da Saga; doc estratégico em `docs/notes/`.

Sem trigger registado em `docs/notes/`, a ordem deste roadmap é a ordem de execução.
