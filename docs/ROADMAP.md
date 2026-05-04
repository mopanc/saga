# Saga — Roadmap

Cada fase explicita: **o que resolve, deliverables, critério de "done", trigger para a fase seguinte**.

> Sem evidência da fase anterior, não há fase nova. Esta regra é absoluta.

---

## Fase 0 — Spec & scaffold ✅ (fechada)

**Resolve:** alinhamento antes de código. Captar raciocínio para não se perder.

**Deliverables:**

- README, DESIGN, ROADMAP (este).
- Decisões em aberto explicitamente listadas.

**Done.** Decisões fechadas:

- ✅ **Stack:** TypeScript.
- ✅ **Tenancy:** single-tenant — *"uma mente, um MCP"*.
- ✅ **Estratégia embeddings:** BM25-only em Fase 1; em Fase 1.5, `EmbeddingProvider` swappable com Ollama (default) e API externa (configurável).

**Trigger Fase 1:** Fase 0 fechada — pronto a arrancar.

---

## Fase 1 — Loop mínimo validado (W1–W2)

**Resolve:** prova a hipótese central — *este sistema reduz repetição de contexto a IAs?*

**Deliverables:**

- `packages/core` mínimo: SQLite + uma tabela (com `embedding BLOB` reservado, NULL nesta fase) + retrieval BM25 via FTS5.
- Interface `EmbeddingProvider` definida em `core` — sem implementação. Garante seam para Fase 1.5.
- `packages/mcp` com 2 tools: `remember`, `recall`.
- Hook `UserPromptSubmit` para Claude Code que injecta top-3 snippets.
- Tests no core: dedup básico, retrieval BM25 correctness.

**O que NÃO entra:**

- Tipos / taxonomia.
- Conflict resolution.
- API REST.
- CLI (excepto `mem export` mínimo, opcional).
- Backfill / multi-IA.

**Done quando (medido em uso real, não em features):**

- 7 dias de uso diário com Claude Code.
- Métrica: repetições de contexto caem semana a semana.
- Pelo menos 3 instâncias documentadas de "IA surpreendeu-me a saber X".

**Triggers:**

- → **Fase 1.5** quando houver ≥3 falhas documentadas de `recall` causadas por limites do BM25 (memória relevante existe mas não foi devolvida porque palavras não bateram).
- → **Fase 2** quando ≥100 memórias capturadas + pelo menos um padrão claro de uso a emergir.

(Independentes — podem disparar em qualquer ordem ou em paralelo.)

---

## Fase 1.5 — Embeddings (quando o keyword falhar)

**Resolve:** retrieval semântico para casos onde BM25 não chega — *"lembras-te quando falámos de X"* sem partilhar palavras com o registo.

**Deliverables:**

- `EmbeddingProvider` com 2 implementações em `packages/core`:
  - `OllamaProvider` (default): `nomic-embed-text` via Ollama local. Zero custo, zero chave.
  - `ExternalAPIProvider`: Voyage `voyage-3-lite` ou OpenAI `text-embedding-3-small`, configurável via env vars.
- Selecção via config (`SAGA_EMBEDDING_PROVIDER=ollama|voyage|openai`) — nunca em código.
- Migration: cria `sqlite-vec` virtual table, backfill de embeddings para memórias existentes.
- `recall` interno passa a híbrido (BM25 + cosine) — assinatura externa não muda.
- Tests: comparar resultados BM25-only vs híbrido em conjunto de queries reais.

**Done quando:**

- Em queries onde BM25 falhava, o híbrido devolve resposta correcta.
- Trocar provider via config funciona sem alterar código.

**Custo de mudar de ideias:** zero. A coluna `embedding` já existia desde Fase 1.

**Trigger Fase 2:** independente desta fase — Fase 2 corre em paralelo se a evidência aparecer primeiro.

---

## Fase 2 — Taxonomia evidence-driven (W3–W4)

**Resolve:** estrutura sobre os dados reais. Distingue ruído de sinal.

**Deliverables:**

- Análise das tags efectivamente usadas (clustering simples).
- Proposta de 4-6 tipos *baseados nos clusters observados* (não nos 12 do whiteboard).
- Migration: adicionar `type`, backfill por cluster.
- Adicionar `provenance` (tool, session_id) — crítico para revogação.
- CLI mínimo: `mem review`, `mem list --type X`, `mem export`.

**Done quando:**

- Tipos derivam de dados, não de intuição.
- `revoke_by_session` funciona.
- Revisão de memórias é manual mas batched (≤5 min/semana).

**Trigger Fase 3:** evidência de contradições reais nos dados.

---

## Fase 3 — Conflito & decay (W5–W6)

**Resolve:** contradições silenciosas que aparecem com tempo.

**Deliverables:**

- Campo `dimension` apenas nos tipos onde aparecem conflitos reais.
- `scope` opcional para preferências context-dependent.
- `stability` tier (`core | slow | fast | volatile`) inferido por tipo.
- Tool `resolve_conflict(id, action)`.
- `last_confirmed_at` actualizado em cada `recall` ou re-asserção.

**Done quando:**

- Contradições conhecidas são detectadas no write, não no read.
- Conflitos `core/slow` bloqueiam e perguntam; `fast/volatile` auto-resolvem.

**Trigger Fase 4:** Fase 1-3 estabilizadas em uso diário.

---

## Fase 4 — Multi-IA ingestion (W7+)

**Resolve:** captura para além do Claude Code. Histórico antigo recuperado.

**Deliverables:**

- Pull adapter para transcripts existentes do Claude Code (ficheiros locais).
- Importer para ChatGPT export (zip).
- Reader para Cursor sqlite local.
- Tool `consolidate_episodes(time_range)` — corre LLM sobre episódios e propõe factos para review queue.

**Done quando:**

- Backfill executável idempotente.
- Confidence + review_status ligados (factos extraídos vão para fila `proposed`).

**Trigger Fase 5:** valor das memórias capturadas multi-IA é ≥ valor das memórias só-Claude.

---

## Fase 5 — API REST & multi-frontend (quando justificado)

**Resolve:** acesso fora de clientes MCP — mobile, dashboards, scripts, IAs sem MCP.

**Deliverables:**

- `packages/api` HTTP REST em `127.0.0.1`, Bearer auth.
- Dashboard web (read-only primeiro).
- Acesso multi-device opcional via Tailscale.

**Done quando:**

- Pelo menos 1 caso de uso real fora-de-MCP a usar a API.

**Anti-trigger:** se não houver caso de uso real, esta fase fica indefinidamente adiada. Sem desperdício.

---

## Futuro / talvez

Apenas registo, sem compromisso:

- Cross-device sync via Litestream (S3-compatible replication).
- Multi-tenant — só se a decisão de produto mudar.
- Tipos extra: `goal`, `relationship`, etc. — só se os dados pedirem.
- Auto-summarisation de episódios longos.
- Plugin Cursor / extensão browser.

> A ausência destes itens da roadmap não é esquecimento — é disciplina.
