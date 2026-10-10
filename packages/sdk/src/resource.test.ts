import { describe, expect, it } from "vitest";
import { DexError, type DexErrorCode } from "./errors.js";
import { createResourceRegistry } from "./registry.js";
import { defineResource } from "./resource.js";
import type { ResourceSpec } from "./types.js";

function expectDexError(fn: () => unknown, code: DexErrorCode, message: string): void {
  let caught: unknown;
  try {
    fn();
  } catch (err) {
    caught = err;
  }
  expect(caught).toBeInstanceOf(DexError);
  expect((caught as DexError).code).toBe(code);
  expect((caught as DexError).message).toBe(message);
}

function setup() {
  const registry = createResourceRegistry();
  return { registry, build: defineResource("container", registry) };
}

const spec = (extra: Partial<ResourceSpec> = {}): ResourceSpec => ({ plugin: "docker", properties: {}, ...extra });

describe("defineResource", () => {
  it("builds a node with the kind as default type and registers it", () => {
    const { registry, build } = setup();
    const node = build("postgres", spec({ properties: { image: "postgres:15" } }));
    expect(node).toEqual({
      name: "postgres",
      type: "container",
      plugin: "docker",
      properties: { image: "postgres:15" },
      dependsOn: [],
    });
    expect(registry.all()).toEqual([node]);
  });

  it("honours a spec.type override", () => {
    expect(setup().build("x", spec({ type: "lxc" })).type).toBe("lxc");
  });

  it("freezes the node, its properties, and its dependencies", () => {
    const node = setup().build("x", spec({ properties: { a: 1 }, dependsOn: ["y"] }));
    expect(Object.isFrozen(node)).toBe(true);
    expect(Object.isFrozen(node.properties)).toBe(true);
    expect(Object.isFrozen(node.dependsOn)).toBe(true);
  });

  it("defaults omitted properties to {}", () => {
    const node = setup().build("x", { plugin: "docker" } as ResourceSpec);
    expect(node.properties).toEqual({});
  });

  it("accepts dotted, dashed, and underscored names", () => {
    expect(setup().build("network.main_1-a", spec()).name).toBe("network.main_1-a");
  });

  it.each([["Postgres"], ["-pg"], [""], [42]])("rejects invalid name %s", (name) => {
    expectDexError(
      () => setup().build(name as string, spec()),
      "INVALID_RESOURCE_NAME",
      `invalid resource name "${String(name)}": must match /^[a-z0-9][a-z0-9._-]*$/`,
    );
  });

  it.each([[""], [7]])("rejects invalid spec.type %s", (type) => {
    expectDexError(
      () => setup().build("x", spec({ type: type as string })),
      "INVALID_RESOURCE_TYPE",
      'resource "x" has an invalid type',
    );
  });

  it.each([[""], [undefined], [1]])("rejects invalid plugin %s", (plugin) => {
    expectDexError(
      () => setup().build("x", spec({ plugin: plugin as string })),
      "INVALID_PLUGIN",
      'resource "x" must declare a non-empty plugin',
    );
  });

  it("accepts nodes and strings as dependencies, deduped and sorted", () => {
    const { build } = setup();
    const b = build("b", spec());
    const node = build("x", spec({ dependsOn: ["c", b, "a", "b", "c"] }));
    expect(node.dependsOn).toEqual(["a", "b", "c"]);
  });

  it.each([[1], [null], [{}], [{ name: 3 }]])("rejects invalid dependency entry %s", (entry) => {
    expectDexError(
      () => setup().build("x", spec({ dependsOn: [entry as unknown as string] })),
      "INVALID_DEPENDENCY",
      'resource "x" has an invalid dependency entry',
    );
  });

  it("does not register a resource that fails validation", () => {
    const { registry, build } = setup();
    expect(() => build("x", spec({ dependsOn: [null as unknown as string] }))).toThrow(DexError);
    expect(registry.has("x")).toBe(false);
  });
});
