import type Database from "better-sqlite3";
import { ulid } from "ulid";
import { RememberInput, RecallInput, type RecallResult } from "./schema.js";

interface RecallRow {
  id: string;
  text: string;
  tags: string;
  source: string | null;
  session_id: string | null;
  created_at: number;
  score: number;
}

export class Memory {
  private readonly insertStmt;
  private readonly recallStmt;
  private readonly countStmt;

  constructor(private readonly db: Database.Database) {
    this.insertStmt = db.prepare(`
      INSERT INTO memory (id, text, tags, source, session_id, created_at)
      VALUES (?, ?, ?, ?, ?, ?)
    `);
    this.recallStmt = db.prepare<[string, number], RecallRow>(`
      SELECT
        m.id, m.text, m.tags, m.source, m.session_id, m.created_at,
        bm25(memory_fts) AS score
      FROM memory_fts
      JOIN memory m ON m.id = memory_fts.id
      WHERE memory_fts MATCH ?
      ORDER BY bm25(memory_fts) ASC
      LIMIT ?
    `);
    this.countStmt = db.prepare<[], { c: number }>(
      "SELECT COUNT(*) AS c FROM memory"
    );
  }

  remember(input: unknown): { id: string } {
    const parsed = RememberInput.parse(input);
    const id = ulid();
    const now = Date.now();
    this.insertStmt.run(
      id,
      parsed.text,
      JSON.stringify(parsed.tags ?? []),
      parsed.source ?? null,
      parsed.sessionId ?? null,
      now
    );
    return { id };
  }

  recall(input: unknown): RecallResult[] {
    const parsed = RecallInput.parse(input);
    const ftsQuery = sanitizeFtsQuery(parsed.query);
    if (ftsQuery === "") return [];
    const rows = this.recallStmt.all(ftsQuery, parsed.k);
    return rows.map((r) => ({
      id: r.id,
      text: r.text,
      tags: JSON.parse(r.tags) as string[],
      source: r.source,
      sessionId: r.session_id,
      createdAt: r.created_at,
      // bm25 returns lower=better; flip so higher=better for callers.
      score: -r.score,
    }));
  }

  count(): number {
    return this.countStmt.get()?.c ?? 0;
  }

  close(): void {
    this.db.close();
  }
}

/**
 * Build a safe FTS5 MATCH expression from arbitrary user text.
 * Strips FTS5 special chars from each token and joins with OR for broad recall.
 */
function sanitizeFtsQuery(input: string): string {
  return input
    .split(/\s+/)
    .map((t) => t.replace(/[^\p{L}\p{N}_]/gu, ""))
    .filter((t) => t.length > 0)
    .map((t) => `"${t}"`)
    .join(" OR ");
}
