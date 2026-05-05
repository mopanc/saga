# Saga — Plan

> Plano de construção da Saga como **mente externa** — canais, camadas, motor lógico, evolução no tempo. Cada iteração tem entregáveis concretos e um teste de utilidade que decide se passa.
>
> Para o modelo cognitivo (camadas, transversais, erros evitados), ver `COGNITIVE_MODEL.md`. Para arquitectura técnica, ver `DESIGN_v2.md`.

## 1. Princípios do plano (não-negociáveis)

1. **Validação por uso domina.** Sem 2-4 semanas de uso real entre iterações grandes, paramos antes de mais código.
2. **Teste de utilidade explícito** em cada iteração. Saímos quando o teste passa, não quando o código compila.
3. **Modelo cognitivo filtrado** é a régua. Cada feature responde a "que camada serve?". Sem resposta — fora.
4. **Documenta a contenção.** Lista do que excluímos vive em `COGNITIVE_MODEL.md §7-8`.
5. **Substrato sobre invenção.** Git, SQLite, OS perms, MCP. Saga não inventa o que já existe.

## 2. Estado actual (baseline)

**O que existe:** branch `pivot/v2-go`, commits `f353f9a → ab20d67`.
- Single binary `saga` com subcomandos: `init`, `mcp`, `hook`, `setup-claude`, `reindex`, `version`
- 4 tools MCP: `recall`, `topic_read`, `topic_list`, `topic_write`
- 2 layers automáticas: `personal` (auto-init) + `project` (auto-discovery via cwd walk-up)
- 39 testes a passar, build limpo
- Hook UserPromptSubmit funcional (mas só faz F3.b — match query → inject; falta F3.a baseline sempre-ligado)

**O que falta para a "mente" estar minimamente viva:**
- Profile (L5) populado com identidade real do user — actualmente vazio
- F3 baseline (lente sempre-ligada) — hook só injecta query matches
- L2 episódica (lembranças) — sistema sem história de pensamento
- L1 sessão — sem cache de actividade recente
- Valência — sem feedback loop

## 3. Iteration 0 — Ground truth

**Objectivo:** colocar o esqueleto cognitivo em uso real, com identidade real do user dentro.

**Entregáveis:**

| # | Acção | Output | Esforço |
|---|---|---|---|
| 0.1 | Profile note completo do user na layer `personal` | `~/.saga/personal/profile/identity.md` (markdown estruturado) | 1h |
| 0.2 | Preferência inicial sobre tom/idioma de comunicação | `personal/preferences/communication.md` | 15m |
| 0.3 | Policy inicial sobre estilo de código (commit, branch, review) | `personal/policy/code-style.md` | 30m |
| 0.4 | `docs/COGNITIVE_MODEL.md` codificado (já feito) | doc canónico | ✓ |
| 0.5 | `docs/PLAN.md` codificado (este doc) | plano canónico | ✓ |
| 0.6 | Notas no `DESIGN_v2.md` apontando divergências cognitivas (não reescrever) | sinaliza onde o doc precisa update após Iteration 1 | 30m |

**Teste de utilidade:**
> Abro o Claude Code numa máquina, faço uma pergunta sobre um produto da minha pipeline. A resposta vem calibrada por mim — o tom, o nível, o contexto — sem eu ter explicado nada nesta sessão. **Critério:** ≥3 testes consecutivos em projectos diferentes com resposta-tua, não resposta-genérica.

**Saída:** ✅ ou ❌. Se ❌, sabemos exactamente onde está o gap (profile fraco, hook não puxa, etc).

## 4. Iteration 1 — Lente sempre-ligada (F3 real)

**Objectivo:** o hook deixa de fazer apenas search-relevance; passa a injectar baseline de identidade *sempre* + tópicos relevantes *quando batem*.

**Entregáveis:**

| # | Acção | Output | Esforço |
|---|---|---|---|
| 1.1 | Função `BuildIdentityBaseline()` — sintetiza profile/preferences/policy num bloco compacto (≤400 tokens) | `internal/saga/baseline.go` | 2h |
| 1.2 | Hook v2 emite duas secções: `<saga-identity>` (sempre) + `<saga-context>` (relevante) | refactor `cmd_hook.go` | 1h |
| 1.3 | Limite configurável de tamanho (`SAGA_BASELINE_MAX_TOKENS`, default 400) | safety | 30m |
| 1.4 | Tests unitários (baseline generation) e e2e (hook output shape) | cobertura | 1h |
| 1.5 | Atualização das descriptions dos tools MCP para reflectir "lente" como função primária | string changes | 30m |

**Teste de utilidade:**
> Durante 3 dias seguidos, abrir Claude Code em qualquer projecto resulta em respostas que assumem o user correctamente sem este se apresentar. Latência adicional do hook < 50ms. Zero crashes. **Critério:** *"sente-se que ele me conhece"* validado em ≥10 sessões consecutivas em ≥3 projectos.

## 5. Iteration 2 — Lembranças (camada episódica)

**Objectivo:** sistema com história de pensamento, não só armazém estático. Cada acto de injecção/recall deixa rasto.

**Entregáveis:**

| # | Acção | Output | Esforço |
|---|---|---|---|
| 2.1 | Migração SQL: tabela `lembranca` (id, topic_id, triggered_at, kind, query, cwd, was_used, outcome) | `migrations/002_lembrancas.sql` | 1h |
| 2.2 | Cada `recall` / `topic_read` / hook injection insere row | instrumentação automática | 2h |
| 2.3 | Tool MCP `lembranca_log` (admin/debug, lista as últimas N com filtros) | observability | 1h |
| 2.4 | Ranking de `recall` ganha factor `recency_of_lembrança` (com timeout configurável) | melhoria do scorer | 1h |
| 2.5 | CLI `saga lembrancas` para inspecção rápida | tooling | 30m |
| 2.6 | Tests unitários e e2e | cobertura | 1h |

**Teste de utilidade:**
> Pergunto à Saga *"que tópicos foram trazidos à conversa esta semana, no projecto acme-platform?"* — recebo resposta com dados reais. **Critério:** rastro completo de uso recolhido sem latência mensurável adicional (<10ms por recall).

## 6. Iteration 3 — Validação prolongada (4 semanas, sem código)

**Objectivo:** detectar o que realmente falta, em vez de adivinhar.

**Sem entregáveis de código.** Apenas observação e medição.

**Métricas a recolher** (manualmente ou via tool de debug a criar à parte):
- Quantas vezes o baseline foi útil vs ruído (auto-feedback ou marcação manual)
- Quantas lembranças foram inúteis (sinal de ranking errado)
- Tópicos criados, modificados, abandonados
- Projectos onde funcionou bem vs mal e porquê
- Tempo médio de "deixar de me repetir a uma IA"

**Saída:** lista priorizada de pain points. **Decide-se com base nela** qual iteração 4+ entra primeiro.

## 7. Iteration 4+ — Conditional, dependentes de evidência da Iteration 3

| Iter | Camada/transversal | Trigger empírico (do que sai da Iter 3) |
|---|---|---|
| 4 | **Valência** (`helpful` / `irrelevant` / `wrong` em lembranças) | Ranking falha consistentemente — utilizador rejeita ≥30% das lembranças |
| 5 | **Sessão** (cache de actividade recente, ~1h) | Sentes que o sistema "esquece" o que fizeste há 30 minutos |
| 6 | **Tipo `skill`** distinto de `policy` | Tens ≥5 patterns "como faço X" recorrentes que não cabem como regras |
| 7 | **Promote workflow** (`topic.promote` real, com PR) | Começas a partilhar com colegas — dept layer activo |
| 8 | **Stale invalidation** com `git blame` | Notas começam a referenciar código que mudou |
| 9 | **Embeddings + sqlite-vec** | BM25 falha 3+ vezes em queries reais (palavras não batem mas a memória existe) |

## 8. Iteration ∞ — Acessórios opcionais

Sem prioridade. Entram só com caso de uso real e demanda externa:
- `saga-ui` (dashboard read-only sobre os ficheiros markdown)
- `saga-gateway` (runtime queries ao projecto vivo, produto separado)
- Importers (transcripts ChatGPT, Cursor, etc.)
- Encryption do personal layer com `age` (se sincronizado)

## 9. Cronograma realista

```
Semana 0      Iteration 0   ground truth + profile real
Semana 1      Iteration 1   lente sempre-ligada (F3 real)
Semana 2      Iteration 2   lembranças
Semanas 3-6   Iteration 3   USO REAL, sem código
Semana 7+     Iteration 4+  decididas por evidência da 3
```

**6 semanas até decisão informada sobre o futuro técnico da Saga.** Antes disso, qualquer "feature seguinte" é especulação.

## 10. Critérios de "pronto" globais

A Saga estará viva como **mente externa funcional** quando:

1. **L5 Identidade** populada e acessível: a IA fala contigo como tu, não como genérico.
2. **L3 Semântica** com massa crítica: ≥20 topic notes em ≥3 projectos, indexadas e recuperáveis.
3. **L2 Episódica** activa: lembranças registadas em todas as injecções, ranking influenciado.
4. **L4 Procedimental** mínimo: ≥5 policies/skills capturados, respeitados pela IA.
5. **L1 Sessão** ou descartada (se evidência mostrar não fazer falta).
6. **F3 Lente** sempre-ligada: zero prompts sem context da Saga em IAs configuradas.
7. **Valência** activa: feedback loop fechado em ≥10 lembranças com outcome.

7 critérios, todos mensuráveis em uso, nenhum em features.

## 11. Roadmap (separado deste plano)

Este doc define **o quê** e **porquê**. O **como granular** (tasks, sequência fina, ownership, deadlines) vive no roadmap, a estruturar a seguir.

> Trigger para começar roadmap: **este plano aprovado pelo user**.
