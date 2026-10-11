import { DexError } from "./errors.js";
import { sanitizeProperties } from "./properties.js";
import type { ResourceCollector } from "./registry.js";
import type { ResourceNode, ResourceSpec } from "./types.js";

export const RESOURCE_NAME_PATTERN = /^[a-z0-9][a-z0-9._-]*$/;
// PLUGIN_NAME_PATTERN constrains plugin names to binary-safe characters.
// Plugins become dex-adapter-<name> binaries on disk.
export const PLUGIN_NAME_PATTERN = /^[a-z0-9][a-z0-9-]*$/;

export type ResourceBuilder = (name: string, spec: ResourceSpec) => ResourceNode;

/** Returns a builder that validates, freezes, and registers resources of `kind`. */
export function defineResource(kind: string, collector: ResourceCollector): ResourceBuilder {
  return (name, spec) => {
    if (typeof name !== "string" || !RESOURCE_NAME_PATTERN.test(name)) {
      throw new DexError(
        "INVALID_RESOURCE_NAME",
        `invalid resource name "${String(name)}": must match ${String(RESOURCE_NAME_PATTERN)}`,
      );
    }
    if (spec.type !== undefined && (typeof spec.type !== "string" || spec.type === "")) {
      throw new DexError("INVALID_RESOURCE_TYPE", `resource "${name}" has an invalid type`);
    }
    if (typeof spec.plugin !== "string" || spec.plugin === "") {
      throw new DexError("INVALID_PLUGIN", `resource "${name}" must declare a non-empty plugin`);
    }
    if (!PLUGIN_NAME_PATTERN.test(spec.plugin)) {
      throw new DexError("INVALID_PLUGIN", `resource "${name}" has an invalid plugin name "${spec.plugin}"`);
    }

    const properties = sanitizeProperties(name, spec.properties ?? {});

    // Targets are not checked here (forward references are legal); buildIR checks them.
    const deps = (spec.dependsOn ?? []).map((entry: unknown) => {
      if (typeof entry === "string") return entry;
      if (entry !== null && typeof entry === "object" && "name" in entry && typeof entry.name === "string") {
        return entry.name;
      }
      throw new DexError("INVALID_DEPENDENCY", `resource "${name}" has an invalid dependency entry`);
    });

    const node: ResourceNode = Object.freeze({
      name,
      type: spec.type ?? kind,
      plugin: spec.plugin,
      properties: Object.freeze(properties),
      // The DAG is a set; sorting keeps the IR deterministic.
      dependsOn: Object.freeze([...new Set(deps)].sort()),
    });
    collector.add(node);
    return node;
  };
}
