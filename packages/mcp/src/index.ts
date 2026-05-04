#!/usr/bin/env node
/**
 * Saga MCP server (stdio transport).
 * Exposes `remember` and `recall` tools to MCP-compatible clients.
 */
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import { Memory, openDatabase, loadConfig } from "@saga/core";
import { tools, dispatch } from "./tools.js";

async function main(): Promise<void> {
  const config = loadConfig();
  const db = openDatabase(config.dbPath);
  const memory = new Memory(db);

  const server = new Server(
    { name: "saga", version: "0.1.0" },
    { capabilities: { tools: {} } }
  );

  server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools }));

  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    return dispatch(memory, req.params.name, req.params.arguments ?? {});
  });

  const transport = new StdioServerTransport();
  await server.connect(transport);

  const shutdown = (): void => {
    memory.close();
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

main().catch((err) => {
  console.error("Saga MCP server failed:", err);
  process.exit(1);
});
