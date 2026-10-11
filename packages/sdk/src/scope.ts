import { createHostEnvSource, envFrom, type EnvSource } from "./env.js";
import { createCommandRegistry, type CommandRegistry } from "./commands.js";
import { DexError } from "./errors.js";
import { createResourceRegistry } from "./registry.js";
import { defineResource, type ResourceBuilder } from "./resource.js";
import { secret } from "./secret.js";
import { buildIR, stableStringify } from "./synth.js";
import type { CommandSpec, DexConfig, DexIR, SecretRef } from "./types.js";

export interface DexScope {
  defineDexConfig(config: DexConfig): DexConfig;
  service: ResourceBuilder;
  volume: ResourceBuilder;
  network: ResourceBuilder;
  env(key: string, fallback?: string): string;
  secret(key: string): SecretRef;
  /** Registers a command (D13); kept separate from resources, so synth output is unchanged. */
  defineCommand(name: string, spec: CommandSpec): void;
  /** The registry backing defineCommand, exposed so the CLI can list and invoke commands. */
  commands: CommandRegistry;
  synth(): DexIR;
  synthJSON(): string;
}

/** One registry + one env source + one config. Members are closures, safe to call unbound. */
export function createDexScope(options?: { envSource?: EnvSource }): DexScope {
  const registry = createResourceRegistry();
  const commands = createCommandRegistry();
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
    secret: (key: string) => secret(key),
    defineCommand: (name: string, spec: CommandSpec): void => commands.define(name, spec),
    commands,
    synth,
    synthJSON: (): string => stableStringify(synth()),
  });
}
