import { describe, expect, it } from "vitest";
import { DexError } from "./errors.js";
import { createResourceRegistry } from "./registry.js";
import type { ResourceNode } from "./types.js";

const node = (name: string, type = "container"): ResourceNode => ({
  name,
  type,
  plugin: "docker",
  properties: {},
  dependsOn: [],
});

describe("createResourceRegistry", () => {
  it("returns nodes in insertion order and reports membership", () => {
    const reg = createResourceRegistry();
    reg.add(node("b"));
    reg.add(node("a"));
    expect(reg.all().map((n) => n.name)).toEqual(["b", "a"]);
    expect(reg.has("a")).toBe(true);
    expect(reg.has("c")).toBe(false);
  });

  it("rejects duplicate names across resource types", () => {
    const reg = createResourceRegistry();
    reg.add(node("x", "container"));
    let caught: unknown;
    try {
      reg.add(node("x", "volume"));
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(DexError);
    expect((caught as DexError).code).toBe("DUPLICATE_RESOURCE");
    expect((caught as DexError).message).toBe('resource "x" is already defined');
  });

  it("returns a frozen snapshot that cannot mutate the registry", () => {
    const reg = createResourceRegistry();
    reg.add(node("a"));
    const all = reg.all() as ResourceNode[];
    expect(() => all.push(node("b"))).toThrow(TypeError);
    expect(reg.all()).toHaveLength(1);
  });
});
