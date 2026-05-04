import { describe, it, expect, beforeEach, afterEach } from "vitest";
import type Database from "better-sqlite3";
import { Memory } from "../src/memory.js";
import { openDatabase } from "../src/db.js";

let db: Database.Database;
let memory: Memory;

beforeEach(() => {
  db = openDatabase(":memory:");
  memory = new Memory(db);
});

afterEach(() => {
  memory.close();
});

describe("Memory.remember", () => {
  it("stores text and returns a ULID", () => {
    const { id } = memory.remember({ text: "Eu prefiro TypeScript." });
    expect(id).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    expect(memory.count()).toBe(1);
  });

  it("stores tags as JSON array", () => {
    memory.remember({
      text: "Saga é local-first.",
      tags: ["principle", "architecture"],
    });
    const results = memory.recall({ query: "local" });
    expect(results).toHaveLength(1);
    expect(results[0]?.tags).toEqual(["principle", "architecture"]);
  });

  it("rejects empty text", () => {
    expect(() => memory.remember({ text: "" })).toThrow();
  });

  it("rejects missing text", () => {
    expect(() => memory.remember({})).toThrow();
  });
});

describe("Memory.recall", () => {
  it("returns empty when DB is empty", () => {
    const results = memory.recall({ query: "qualquer" });
    expect(results).toEqual([]);
  });

  it("finds memories by keyword", () => {
    memory.remember({ text: "Trabalho na Balanças Marques desde 2021." });
    memory.remember({ text: "Adoro caminhar à beira-mar." });
    memory.remember({ text: "TypeScript é a minha linguagem preferida." });

    const results = memory.recall({ query: "TypeScript" });
    expect(results).toHaveLength(1);
    expect(results[0]?.text).toContain("TypeScript");
  });

  it("respects k limit", () => {
    for (let i = 0; i < 10; i++) {
      memory.remember({ text: `nota número ${i} com palavra-chave saga` });
    }
    const results = memory.recall({ query: "saga", k: 3 });
    expect(results).toHaveLength(3);
  });

  it("orders by relevance score (more matches first)", () => {
    memory.remember({ text: "Apenas uma menção breve a TypeScript." });
    memory.remember({
      text: "TypeScript TypeScript TypeScript em todo o lado.",
    });

    const results = memory.recall({ query: "TypeScript" });
    expect(results).toHaveLength(2);
    expect(results[0]!.score).toBeGreaterThan(results[1]!.score);
  });

  it("matches diacritic-insensitively", () => {
    memory.remember({ text: "Memória de longo prazo." });
    const results = memory.recall({ query: "memoria" });
    expect(results.length).toBeGreaterThanOrEqual(1);
  });

  it("survives queries with FTS5 special characters", () => {
    memory.remember({ text: "Hello world." });
    const results = memory.recall({ query: "x*y(z)" });
    expect(Array.isArray(results)).toBe(true);
  });

  it("returns empty when query has only special chars", () => {
    memory.remember({ text: "Hello world." });
    const results = memory.recall({ query: "***" });
    expect(results).toEqual([]);
  });

  it("populates source and timestamp", () => {
    memory.remember({ text: "test", source: "claude-code", sessionId: "s1" });
    const results = memory.recall({ query: "test" });
    expect(results[0]?.source).toBe("claude-code");
    expect(results[0]?.sessionId).toBe("s1");
    expect(results[0]?.createdAt).toBeGreaterThan(0);
  });
});
