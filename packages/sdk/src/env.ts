import { DexError } from "./errors.js";

/** goja has no Node process object; the host injects a plain env object under this global. */
export const HOST_ENV_GLOBAL = "__dex_env";

export interface EnvSource {
  get(key: string): string | undefined;
}

export function createHostEnvSource(
  scope: Record<string, unknown> = globalThis as unknown as Record<string, unknown>,
): EnvSource {
  return Object.freeze({
    get(key: string): string | undefined {
      // Read on every call: the host may populate the global after module init.
      const vars = scope[HOST_ENV_GLOBAL];
      if (vars === null || typeof vars !== "object") return undefined;
      const value = (vars as Record<string, unknown>)[key];
      return typeof value === "string" ? value : undefined;
    },
  });
}

export function envFrom(source: EnvSource, key: string, fallback?: string): string {
  const value = source.get(key);
  if (typeof value === "string") return value;
  if (fallback !== undefined) return fallback;
  throw new DexError("MISSING_ENV", `environment variable "${key}" is not set and no fallback was provided`);
}
