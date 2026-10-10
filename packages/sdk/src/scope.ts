import { createHostEnvSource, envFrom, type EnvSource } from "./env.js";
import { DexError } from "./errors.js";
import { createResourceRegistry } from "./registry.js";
import { defineResource, type ResourceBuilder } from "./resource.js";
import { buildIR, stableStringify } from "./synth.js";
import type { DexConfig, DexIR } from "./types.js";

export interface DexScope {
  defineDexConfig(config: DexConfig): DexConfig;
  service: ResourceBuilder;
  volume: ResourceBuilder;
  network: ResourceBuilder;
  env(key: string, fallback?: string): string;
  synth(): DexIR;
  synthJSON(): string;
}

/** One registry + one env source + one config. Members are closures, safe to call unbound. */
export function createDexScope(options?: { envSource?: EnvSource }): DexScope {
  const registry = createResourceRegistry();
  const envSource = options?.envSource ?? createHostEnvSource();
  let config: DexConfig | undefined;

  const synth = (): DexIR => {
    if (config === undefined) {
      throw new DexError("MISSING_CONFIG", "no dex config defined: call defineDexConfig() in dex.config.ts");
    }
    return buildIR(config, registry.all());
  };

  return Object.freeze({
    defineDexConfig(input: DexConfig): DexConfig {
      if (config !== undefined) {
        throw new DexError("DUPLICATE_CONFIG", "defineDexConfig() was called more than once");
      }
      config = Object.freeze({ ...input });
      return config;
    },
    service: defineResource("container", registry),
    volume: defineResource("volume", registry),
    network: defineResource("network", registry),
    env: (key: string, fallback?: string): string => envFrom(envSource, key, fallback),
    synth,
    synthJSON: (): string => stableStringify(synth()),
  });
}
