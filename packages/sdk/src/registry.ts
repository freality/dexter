import { DexError } from "./errors.js";
import type { ResourceNode } from "./types.js";

export interface ResourceCollector {
  add(node: ResourceNode): void;
  has(name: string): boolean;
  all(): readonly ResourceNode[];
}

/** Names are unique across all resource types, not per type. */
export function createResourceRegistry(): ResourceCollector {
  const nodes = new Map<string, ResourceNode>();
  return Object.freeze({
    add(node: ResourceNode): void {
      if (nodes.has(node.name)) {
        throw new DexError("DUPLICATE_RESOURCE", `resource "${node.name}" is already defined`);
      }
      nodes.set(node.name, node);
    },
    has(name: string): boolean {
      return nodes.has(name);
    },
    all(): readonly ResourceNode[] {
      return Object.freeze([...nodes.values()]);
    },
  });
}
