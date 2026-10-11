import { DexError } from "./errors.js";
import type { SecretRef } from "./types.js";

/**
 * Declares a reference to a secret resolved from the environment by the engine.
 * Does NOT check whether the key is present — that is the engine's job.
 *
 * @param key - The environment variable name holding the secret value.
 * @returns A frozen SecretRef object for use in resource properties.
 */
export function secret(key: string): SecretRef {
  if (typeof key !== "string" || key === "") {
    throw new DexError(
      "INVALID_SECRET",
      `secret() key must be a non-empty string, got ${typeof key === "string" ? "empty string" : typeof key}`,
    );
  }
  return Object.freeze({ $dex_type: "secret" as const, provider: "env" as const, key });
}