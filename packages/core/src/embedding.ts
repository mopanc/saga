/**
 * EmbeddingProvider — Phase 1.5 seam.
 *
 * Phase 1 ships no implementation; the interface lives here so future
 * providers (Ollama, Voyage, OpenAI) plug in without touching call sites.
 */
export interface EmbeddingProvider {
  readonly dimension: number;
  readonly modelId: string;
  embed(text: string): Promise<Float32Array>;
}
