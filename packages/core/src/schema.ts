import { z } from "zod";

export const RememberInput = z.object({
  text: z.string().min(1),
  tags: z.array(z.string()).optional(),
  source: z.string().optional(),
  sessionId: z.string().optional(),
});
export type RememberInput = z.infer<typeof RememberInput>;

export const RecallInput = z.object({
  query: z.string().min(1),
  k: z.number().int().positive().max(50).default(5),
});
export type RecallInput = z.infer<typeof RecallInput>;

export interface MemoryRecord {
  id: string;
  text: string;
  tags: string[];
  source: string | null;
  sessionId: string | null;
  createdAt: number;
}

export interface RecallResult extends MemoryRecord {
  score: number;
}
