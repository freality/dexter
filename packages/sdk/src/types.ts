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
