import { DexError } from "./errors.js";
import type { DexConfig, DexIR, ResourceIR, ResourceNode } from "./types.js";

/** Assembles the SPEC.md §5.1 IR, validating the project and dependency targets. */
export function buildIR(config: DexConfig, resources: readonly ResourceNode[]): DexIR {
  if (typeof config.project !== "string" || config.project === "") {
    throw new DexError("INVALID_PROJECT", "invalid project name: must be a non-empty string");
  }

  const byName = new Map(resources.map((node) => [node.name, node]));
  const out: Record<string, ResourceIR> = {};
  for (const name of [...byName.keys()].sort()) {
    const node = byName.get(name)!;
    for (const target of node.dependsOn) {
      if (target === node.name) {
        throw new DexError("INVALID_DEPENDENCY", `resource "${node.name}" cannot depend on itself`);
      }
      if (!byName.has(target)) {
        throw new DexError("INVALID_DEPENDENCY", `resource "${node.name}" depends on unknown resource "${target}"`);
      }
    }
    out[node.name] = {
      type: node.type,
      plugin: node.plugin,
      properties: node.properties,
      depends_on: [...node.dependsOn],
    };
  }
  return { project: config.project, resources: out };
}

/** JSON.stringify with every object's keys sorted; byte-stable for state hashing. */
export function stableStringify(value: unknown): string {
  return JSON.stringify(value, (_key, val: unknown) =>
    val !== null && typeof val === "object" && !Array.isArray(val)
      ? Object.fromEntries(
          Object.keys(val)
            .sort()
            .map((k) => [k, (val as Record<string, unknown>)[k]]),
        )
      : val,
  );
}
