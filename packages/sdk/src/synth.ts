import { DexError } from "./errors.js";
import type { DexConfig, DexIR, ResourceIR, ResourceNode } from "./types.js";

// PLUGIN_SPEC_PATTERN validates plugin spec strings.
// Format: github.com/<owner>/<repo>[@tag]
var PLUGIN_SPEC_PATTERN = /^github\.com\/[a-zA-Z0-9][a-zA-Z0-9._-]*\/[a-zA-Z0-9][a-zA-Z0-9._-]*(@[^@]+)?$/;

/** Assembles the SPEC.md §5.1 IR, validating the project and dependency targets. */
export function buildIR(config: DexConfig, resources: readonly ResourceNode[]): DexIR {
  if (typeof config.project !== "string" || config.project === "") {
    throw new DexError("INVALID_PROJECT", "invalid project name: must be a non-empty string");
  }

  // Validate and deduplicate plugins (order-preserving).
  var plugins: string[] = [];
  if (config.plugins !== undefined) {
    if (!Array.isArray(config.plugins)) {
      throw new DexError("INVALID_PLUGIN_SPEC", "plugins must be an array of strings");
    }
    var seen = new Set<string>();
    for (var i = 0; i < config.plugins.length; i++) {
      var spec = config.plugins[i];
      if (typeof spec !== "string" || !PLUGIN_SPEC_PATTERN.test(spec)) {
        throw new DexError(
          "INVALID_PLUGIN_SPEC",
          'invalid plugin spec "' + spec + '": must match github.com/<owner>/<repo>[@tag]',
        );
      }
      if (!seen.has(spec)) {
        seen.add(spec);
        plugins.push(spec);
      }
    }
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
  return { project: config.project, plugins, resources: out };
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
