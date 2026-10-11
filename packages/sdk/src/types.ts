export type IRValue = string | number | boolean | null | IRValue[] | { [key: string]: IRValue };

/** One resource as serialized for the Go engine (SPEC.md §5.1). */
export interface ResourceIR {
  type: string;
  plugin: string;
  properties: Record<string, IRValue>;
  /** Always present; empty array when the resource has no dependencies. */
  depends_on: string[];
}

export interface DexIR {
  project: string;
  resources: Record<string, ResourceIR>;
}

export interface DexConfig {
  project: string;
}

export interface ResourceNode {
  readonly name: string;
  readonly type: string;
  readonly plugin: string;
  readonly properties: Record<string, IRValue>;
  readonly dependsOn: readonly string[];
}

export interface ResourceSpec {
  type?: string;
  plugin: string;
  properties: Record<string, IRValue>;
  dependsOn?: readonly (ResourceNode | string)[];
}
export interface SecretRef {
  readonly $dex_type: "secret";
  readonly provider: "env";
  readonly key: string;
}

/** Runtime context handed to a command's run() by `dex run` (D13). */
export interface CommandContext {
  readonly args: readonly string[];
  env(key: string): string | undefined;
  /** Resource state exactly as persisted in state.json; secret properties stay `{ $dex_type, provider, key, sha256 }` until secret() is called. */
  readonly resources: Readonly<Record<string, IRValue>>;
  /** Plaintext of a .env/process-env value, the same source env() uses. */
  secret(key: string): string;
  /** Runs argv as a host process with stdio attached, returning its exit code. */
  exec(argv: readonly string[]): number;
}

export interface CommandSpec {
  description?: string;
  run(ctx: CommandContext): number | void;
}

/** One registered command as exposed to the Go runner via the __dex_commands global. */
export interface CommandEntry {
  name: string;
  readonly description: string;
  readonly run: (ctx: CommandContext) => number | void;
}
