// Public surface of @dex/iac. Zero Node.js dependencies: this bundle runs
// inside goja, so the host injects env vars via `globalThis.__dex_env` and
// reads the IR via `globalThis.__dex_synth()`.
import { createDexScope } from "./scope.js";

export type * from "./types.js";
export { DexError } from "./errors.js";
export type { DexErrorCode } from "./errors.js";
export { HOST_ENV_GLOBAL, createHostEnvSource, envFrom } from "./env.js";
export type { EnvSource } from "./env.js";
export { createResourceRegistry } from "./registry.js";
export type { ResourceCollector } from "./registry.js";
export { sanitizeProperties } from "./properties.js";
export { RESOURCE_NAME_PATTERN, defineResource } from "./resource.js";
export type { ResourceBuilder } from "./resource.js";
export { buildIR, stableStringify } from "./synth.js";
export { createDexScope };
export type { DexScope } from "./scope.js";

export const SYNTH_GLOBAL = "__dex_synth";

const defaultScope = createDexScope();

export const defineDexConfig = defaultScope.defineDexConfig;
export const service = defaultScope.service;
export const volume = defaultScope.volume;
export const network = defaultScope.network;
export const env = defaultScope.env;
export const synth = defaultScope.synth;

(globalThis as unknown as Record<string, unknown>)[SYNTH_GLOBAL] = defaultScope.synthJSON;
