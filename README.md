# Saga — Personal Memory MCP

Camada de memória pessoal local-first, partilhada entre IAs (Claude, ChatGPT, Cursor, etc.) via MCP. O LLM faz o raciocínio; a Saga é a tua memória de longo prazo.

**Estado:** Fase 1 em curso (loop mínimo validado).

## Filosofia em três pontos

1. **Invisibilidade é o produto.** O sistema só é bom se melhorar o raciocínio diário sem te obrigar a pensar nele.
2. **Taxonomia emerge dos dados, não do whiteboard.** Semana 1 é texto raw + tags livres; a estrutura aparece a partir de uso real.
3. **Local-first, soberania total.** SQLite no teu disco, zero cloud por default, export portável JSON sempre disponível.

## Documentação

- [Design](docs/DESIGN.md) — arquitectura, estrutura do projecto, modelo de dados, tools, decisões.
- [Roadmap](docs/ROADMAP.md) — fases com o que cada uma resolve.

## Decisões fechadas

- **Stack:** TypeScript.
- **Tenancy:** single-tenant — *"uma mente, um MCP"*.
- **Embeddings:** BM25-only em Fase 1; em Fase 1.5, `EmbeddingProvider` swappable (Ollama default, API externa configurável).

## Decisões em aberto

- **Backfill Fase 4:** prioridade de adapters (Claude Code transcripts vs ChatGPT export vs Cursor) — adiada para Fase 4.

## Quick start

Pré-requisitos: Node ≥20, npm.

```bash
npm install
npm run build
npm test
```

Base de dados em `~/.saga/memory.db` por default. Override via `SAGA_DB_PATH`.

### Integrar com o Claude Code

Em `~/.claude/settings.json`:

```json
{
  "mcpServers": {
    "saga": {
      "command": "node",
      "args": ["/CAMINHO/ABSOLUTO/saga/packages/mcp/dist/index.js"]
    }
  },
  "hooks": {
    "UserPromptSubmit": [{
      "hooks": [{
        "type": "command",
        "command": "node /CAMINHO/ABSOLUTO/saga/packages/mcp/dist/hook-recall.js"
      }]
    }]
  }
}
```

O servidor MCP expõe duas tools (`remember`, `recall`). O hook injecta os top-3 snippets relevantes a cada prompt — sem teres de pedir.

## Próximo passo

`docs/ROADMAP.md` → Fase 1.5 (Embeddings) só dispara quando o BM25 falhar em casos reais.
