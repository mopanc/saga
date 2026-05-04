import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import type { Memory } from "@saga/core";

export const tools = [
  {
    name: "remember",
    description:
      "Save a memory about the user (preferences, projects, decisions, identity, facts). " +
      "Call this whenever the user reveals something durable that future conversations should " +
      "know — even across other AI tools. The memory is stored locally and shared via Saga.",
    inputSchema: {
      type: "object",
      properties: {
        text: {
          type: "string",
          description: "The memory content. Free text, full sentences preferred.",
        },
        tags: {
          type: "array",
          items: { type: "string" },
          description: "Optional free-form tags (e.g., 'preference', 'project:saga').",
        },
        source: {
          type: "string",
          description: "Optional origin marker (e.g., 'claude-code', 'cursor').",
        },
      },
      required: ["text"],
    },
  },
  {
    name: "recall",
    description:
      "Search Saga's local memory for relevant snippets. Call this BEFORE answering questions " +
      "about the user's preferences, projects, past decisions, identity, or any context you " +
      "might lack. Returns top-k matching memories ranked by relevance.",
    inputSchema: {
      type: "object",
      properties: {
        query: {
          type: "string",
          description: "Search query (keywords or short phrase).",
        },
        k: {
          type: "number",
          description: "Maximum number of results (default 5, max 50).",
          minimum: 1,
          maximum: 50,
        },
      },
      required: ["query"],
    },
  },
] as const;

export function dispatch(
  memory: Memory,
  name: string,
  args: Record<string, unknown>
): CallToolResult {
  switch (name) {
    case "remember": {
      const { id } = memory.remember(args);
      return {
        content: [{ type: "text", text: `Saved memory ${id}.` }],
        structuredContent: { id },
      };
    }
    case "recall": {
      const results = memory.recall(args);
      if (results.length === 0) {
        return { content: [{ type: "text", text: "No matching memories." }] };
      }
      const formatted = results
        .map(
          (r, i) =>
            `${i + 1}. [score=${r.score.toFixed(3)}] ${r.text}` +
            (r.tags.length > 0 ? ` (${r.tags.join(", ")})` : "")
        )
        .join("\n");
      return {
        content: [{ type: "text", text: formatted }],
        structuredContent: { results },
      };
    }
    default:
      throw new Error(`Unknown tool: ${name}`);
  }
}
