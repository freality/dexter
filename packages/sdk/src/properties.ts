import { DexError } from "./errors.js";
import type { IRValue } from "./types.js";

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object") return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

/**
 * Validates and deep-clones resource properties so they serialize to JSON
 * losslessly. Object keys holding `undefined` are dropped (as JSON.stringify
 * does); everything JSON.stringify would silently mangle is rejected.
 */
export function sanitizeProperties(resourceName: string, properties: unknown): Record<string, IRValue> {
  if (!isPlainObject(properties)) {
    throw new DexError("INVALID_PROPERTY", `resource "${resourceName}" properties must be an object`);
  }

  const ancestors = new Set<object>();

  const reject = (path: string, descriptor: string): never => {
    throw new DexError(
      "INVALID_PROPERTY",
      `resource "${resourceName}" property "${path}" is not JSON-serializable (${descriptor})`,
    );
  };

  const enter = (path: string, value: object): void => {
    if (ancestors.has(value)) {
      throw new DexError(
        "INVALID_PROPERTY",
        `resource "${resourceName}" property "${path}" contains a circular reference`,
      );
    }
    ancestors.add(value);
  };

  const cloneObject = (obj: Record<string, unknown>, prefix: string): Record<string, IRValue> => {
    const out: Record<string, IRValue> = {};
    for (const [key, val] of Object.entries(obj)) {
      if (val === undefined) continue;
      out[key] = clone(val, prefix === "" ? key : `${prefix}.${key}`);
    }
    return out;
  };

  const clone = (value: unknown, path: string): IRValue => {
    switch (typeof value) {
      case "string":
      case "boolean":
        return value;
      case "number":
        return Number.isFinite(value) ? value : reject(path, "non-finite number");
      case "object": {
        if (value === null) return null;
        if (!Array.isArray(value) && !isPlainObject(value)) return reject(path, "non-plain object");
        enter(path, value);
        const out = Array.isArray(value)
          ? Array.from(value, (item, i) => clone(item, `${path}[${i}]`)) // visits holes as undefined
          : cloneObject(value, path);
        ancestors.delete(value);
        return out;
      }
      default:
        return reject(path, typeof value);
    }
  };

  ancestors.add(properties);
  return cloneObject(properties, "");
}
