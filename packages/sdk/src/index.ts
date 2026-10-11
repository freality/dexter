// Public surface of @dex/iac. Zero Node.js dependencies: this bundle runs
// inside goja, so the host injects env vars via `globalThis.__dex_env` and
// reads the IR via `globalThis.__dex_synth()`.
import { createDexScope } from "./scope.js";

export type * from "./types.js";
export { DexError } from "./errors.js";
export type { DexErrorCode } from "./errors.js";

export const SYNTH_GLOBAL = "__dex_synth";
export const COMMANDS_GLOBAL = "__dex_commands";

const defaultScope = createDexScope();

export const defineDexConfig = defaultScope.defineDexConfig;
export const service = defaultScope.service;
export const volume = defaultScope.volume;
export const network = defaultScope.network;
export const env = defaultScope.env;
export const secret = defaultScope.secret;
export const defineCommand = defaultScope.defineCommand;
export const synth = defaultScope.synth;

(globalThis as unknown as Record<string, unknown>)[SYNTH_GLOBAL] = defaultScope.synthJSON;

// The Go side (dex run) reads registered commands through this global: a
// function returning every entry (name, description, run), in name order.
(globalThis as unknown as Record<string, unknown>)[COMMANDS_GLOBAL] = () =>
  defaultScope.commands.names().map((name) => {
    const spec = defaultScope.commands.get(name)!;
    return { name, description: spec.description ?? "", run: spec.run };
  });
