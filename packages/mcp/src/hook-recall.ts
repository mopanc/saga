#!/usr/bin/env node
/**
 * Claude Code UserPromptSubmit hook for Saga.
 *
 * Reads the prompt event JSON from stdin, queries Saga for relevant memories,
 * and emits a context block on stdout that Claude Code prepends to the prompt.
 *
 * Register in ~/.claude/settings.json:
 *
 *   {
 *     "hooks": {
 *       "UserPromptSubmit": [{
 *         "hooks": [{
 *           "type": "command",
 *           "command": "node /abs/path/to/saga/packages/mcp/dist/hook-recall.js"
 *         }]
 *       }]
 *     }
 *   }
 */
import { Memory, openDatabase, loadConfig } from "@saga/core";

const TOP_K = 3;

async function readStdin(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk as Buffer);
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function main(): Promise<void> {
  const raw = await readStdin();
  let prompt = "";
  try {
    const event = JSON.parse(raw) as { prompt?: string };
    prompt = event.prompt ?? "";
  } catch {
    prompt = raw;
  }
  if (!prompt.trim()) return;

  const config = loadConfig();
  const db = openDatabase(config.dbPath);
  const memory = new Memory(db);

  try {
    const hits = memory.recall({ query: prompt, k: TOP_K });
    if (hits.length === 0) return;

    const block =
      "<saga-memory>\n" +
      hits.map((h) => `- ${h.text}`).join("\n") +
      "\n</saga-memory>\n";

    process.stdout.write(block);
  } finally {
    memory.close();
  }
}

main().catch((err) => {
  // Fail silent — never block prompts on hook errors.
  process.stderr.write(
    `saga hook error: ${err instanceof Error ? err.message : String(err)}\n`
  );
});
