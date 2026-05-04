import { homedir } from "node:os";
import { join } from "node:path";

export interface SagaConfig {
  dbPath: string;
}

export function loadConfig(overrides: Partial<SagaConfig> = {}): SagaConfig {
  const defaultDir = join(homedir(), ".saga");
  return {
    dbPath: process.env["SAGA_DB_PATH"] ?? join(defaultDir, "memory.db"),
    ...overrides,
  };
}
