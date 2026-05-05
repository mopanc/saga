# Saga — Cognitive Model

> Vocabulário e decomposição funcional da Saga inspirados em primitivas cognitivas reais (neurociência, psicologia cognitiva). **Inspiração, nunca implementação.** Cada conceito adoptado tem que justificar-se por necessidade de produto, não por semelhança ao cérebro.
>
> Este doc é fonte de verdade para *o que cada parte da Saga representa funcionalmente*. Para arquitectura técnica, ver `DESIGN_v2.md`. Para iterações de construção, ver `PLAN.md`.

## 1. Missão (versão filtrada)

> Saga é a memória externa estruturada por camadas — identidade, procedimentos, conhecimento, lembranças, sessão — acessível a qualquer IA via lente automática. A IA é o motor; a Saga é o que torna a resposta tua, não do mundo.

**Sem teatro:** sem "consciência", sem "subconsciente", sem "mente artificial". Cada termo que sobrevive diz coisa real e é mensurável.

## 2. As 5 camadas

```
┌───────────────────────────────────────────────────────────┐
│  L5 — IDENTIDADE       (lenta, profunda, raramente muda)  │
│       Quem és. Bio, capacidades estáveis, valores,        │
│       línguas, contexto vital.                            │
│       Saga: tipo `profile`, layer `personal`              │
├───────────────────────────────────────────────────────────┤
│  L4 — PROCEDIMENTAL    (média, padrões de acção)          │
│       Como fazes. Regras (`policy`) e métodos (`skill`).  │
│       "Sempre commit estilo conventional", "abordo bugs   │
│       isolando primeiro o input minimal".                 │
│       Saga: tipos `policy` + `skill` (skill em P1)        │
├───────────────────────────────────────────────────────────┤
│  L3 — SEMÂNTICA        (média, factos durados)            │
│       O que aprendeste sobre coisas específicas.          │
│       Topic notes auto-contidas por área/projecto.        │
│       Saga: tipo `topic`                                  │
├───────────────────────────────────────────────────────────┤
│  L2 — EPISÓDICA        (rápida, eventual)                 │
│       O que aconteceu. Lembranças (eventos de recall),    │
│       decisões datadas, sessões.                          │
│       Saga: tabela `lembranca` + git log                  │
├───────────────────────────────────────────────────────────┤
│  L1 — SESSÃO           (transitória, ~horas)              │
│       Em que estás agora. Projecto activo, ficheiros      │
│       tocados, queries da última hora.                    │
│       Saga: cache de actividade recente, não persiste     │
└───────────────────────────────────────────────────────────┘
```

**Distinção PT-PT que matters:**
- **Memória** (substantivo de armazenamento) = registo durável. O ficheiro `.md`, a row em `topic_index`. Existe sempre.
- **Lembrança** (substantivo de evento) = um acto de trazer uma memória ao presente. Datado, atribuível, com efeito. Tem sua própria tabela.

Em inglês colapsa-se em "memory". Não em PT, e não na Saga.

## 3. As 2 transversais

```
┌─────────────────────────────────────┐
│  ATENÇÃO / LENTE                    │
│  Função do hook. Selecciona e       │
│  injecta no prompt:                 │
│    (a) baseline da identidade       │
│        (sempre, ~400 tokens)        │
│    (b) tópicos relevantes à query   │
│        (até K matches)              │
│  Critério: relevância × recência ×  │
│  valência × sessão activa.          │
└─────────────────────────────────────┘

┌─────────────────────────────────────┐
│  VALÊNCIA                           │
│  Tag em cada lembrança após uso:    │
│    helpful | irrelevant | wrong     │
│  Pesa selecção em invocações        │
│  futuras. Sem "emoções"; só feedback│
│  com efeito.                        │
└─────────────────────────────────────┘
```

## 4. Canais (input/output da mente)

Não é metáfora — é a topologia real:

**Input:**
- `hook stdin` — evento `{prompt, cwd}` do Claude Code
- `MCP tools/call` — chamadas `recall`, `topic_read`, `topic_list`, `topic_write`
- `CLI` — `saga reindex`, `saga init`, etc.
- `git pull` — sincronização cross-machine das layers

**Output:**
- `hook stdout` — `<saga-meta>` (sempre) + `<saga-identity>` (se há profile/preferences) + `<saga-context>` (se há topic match) injectados no prompt
- `MCP tools/call response` — resultados estruturados para a IA
- `CLI stdout` — output humano-legível
- `git push` — propagação cross-machine

**Storage (substrato):**
- Markdown em ficheiros (fonte de verdade)
- SQLite (índice descartável, regenerável)
- Git (versão, sync, audit)

## 5. Motor lógico (selecção, ranking, decisão)

O que decide *o que sobe à lente* a cada momento:

**Hoje (v2 actual):**
```
score = bm25(topic_fts MATCH query)
filter by active_scopes
limit K
```

**Após Iteration 1 (lente sempre-ligada):**
```
inject_always = profile_summary  // L5 + parts of L4
inject_relevant = top_K(score) where score > threshold
```

**Após Iteration 2 (lembranças):**
```
score = bm25 + recency_weight(last_lembrança_at)
```

**Após Iteration 4 (valência):**
```
score = bm25 + recency_weight + valence_weight
where valence_weight: helpful=+1, irrelevant=-0.5, wrong=-2
```

Cada novo factor entra **só com evidência** de que o anterior é insuficiente.

## 6. Evolução no tempo

A Saga não é estática. Quatro modos de mudança:

| Modo | O que faz | Quando | Quem activa |
|---|---|---|---|
| **Crescimento** | Novos topic notes; profile expandido | Quando a IA descobre algo durável | IA (com aprovação ou auto se em personal) |
| **Histórico** | Lembranças acumulam | A cada injecção | Sistema (automático) |
| **Feedback** | Valência marcada nas lembranças | Após uso | Utilizador (ou inferência futura) |
| **Poda** | Notas sem lembrança há N meses → archive (não delete) | Comando manual ou pre-recall lint | Utilizador |
| **Consolidação** | Várias lembranças episódicas + topic notes → topic note semântica nova | Comando manual `saga consolidate` | Utilizador, IA propõe |

## 7. Erros conhecidos da replicação cérebro→máquina (e como evitamos)

Estes não são erros teóricos — são erros documentados em décadas de IA. Listamos para pré-comprometer-nos a **não os repetir**.

| Erro | Como o evitamos |
|---|---|
| **Implementação por analogia** ("tem porque o cérebro tem") | Cada conceito passa pelo crivo: *"Que problema concreto resolve para o user?"* Sem resposta clara → fora |
| **Constrangimentos biológicos forçados** | Sem 7±2 working memory, sem half-life biológico, sem ciclos de sono |
| **Emergente vs desenhado** | Saga é desenhada limpa; não replica caos cerebral só para "parecer real" |
| **Confusão de níveis (Marr)** | Cérebro inspira nível **computacional** (que função?). Algorítmico e implementação ficam pelos requisitos de engenharia. |
| **Single-system fallacy** | Não forçamos uma classe = uma região cerebral. Camadas são funcionais, não anatómicas. |
| **Romantização** | "É como o cérebro" não é argumento; é hipótese. Argumentos: utilidade ao user, evidência empírica, ergonomia. |
| **Subestimar engenharia** | Backups, audit, security, tests, monitoring — todos prioritários ao lado do modelo |
| **Treating analogy as proof** | Cada feature inspirada em cognição é validada *empiricamente* em uso real. |
| **Anthropomorphism** | "Memory" no computador ≠ memória cerebral. Mesma palavra, processo diferente. Linguagem precisa. |
| **Wrong abstraction level** | Saga é tool, não simulador. Funções de produto > fidelidade biológica. |

## 8. O que **NÃO está na Saga** (anti-creep)

Foram considerados e excluídos por falharem o crivo "que problema do user resolve concretamente?":

| Conceito | Porquê fora |
|---|---|
| **Schemas explícitos** (templates de situação) | Sem caso de uso concreto ainda. Reentra se aparecer pattern de "sempre que faço X, sigo template Y" repetido. |
| **Priming associativo** (graph de relações entre tópicos) | "Synonyms" em frontmatter cobre 80%. Grafo dedicado — esperar evidência. |
| **Consolidação automática** | Cron job a "consolidar memórias" é teatro. Manual via `saga consolidate` quando justificar. |
| **Decay biológico** (half-life de notas) | "Stale" detectado por `last_lembrança_at` + git blame, não por simulação de esquecimento. |
| **Emoções/afecto rico** | Valência reduzida a 3 valores funcionais. Sem rede emocional. |
| **REM/sleep cycles** | Teatro puro. Background jobs têm que justificar-se por ROI mensurável, não por "consolidação cerebral". |
| **Subconsciente como entidade** | "Subconsciente" = "tudo o que está na memória mas não foi seleccionado pela lente *desta vez*". Não precisa de classe própria. |

## 9. Glossário

| Termo | Definição precisa |
|---|---|
| **Camada** | Conjunto funcionalmente coerente de memórias (não é localização anatómica) |
| **Lente** | Função que selecciona o subconjunto de memórias a injectar num dado prompt |
| **Lembrança** | Evento datado de uma memória ser injectada/usada |
| **Memória** | Registo durável (ficheiro .md + row no índice) |
| **Scope** | Camada de visibilidade (personal, project, dept, org) — ortogonal às camadas cognitivas |
| **Tipo** | Categoria funcional dentro do conhecimento (profile, preference, policy, skill, topic) |
| **Valência** | Tag funcional pós-uso (helpful, irrelevant, wrong) |

---

**Este doc é normativo.** Qualquer feature proposta para a Saga tem que situar-se neste modelo e justificar-se contra a lista da §7. Quando em dúvida sobre adicionar algo: *é teatro?* Se sim, fora.
