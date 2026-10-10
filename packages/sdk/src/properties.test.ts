import { describe, expect, it } from "vitest";
import { DexError } from "./errors.js";
import { sanitizeProperties } from "./properties.js";

function expectDexError(fn: () => unknown, message: string): void {
  let caught: unknown;
  try {
    fn();
  } catch (err) {
    caught = err;
  }
  expect(caught).toBeInstanceOf(DexError);
  expect((caught as DexError).code).toBe("INVALID_PROPERTY");
  expect((caught as DexError).message).toBe(message);
}

const notSerializable = (path: string, descriptor: string): string =>
  `resource "r" property "${path}" is not JSON-serializable (${descriptor})`;

describe("sanitizeProperties", () => {
  it("deep-clones so later caller mutations do not leak into the IR", () => {
    const input = { env: { A: "1" }, ports: [80] };
    const out = sanitizeProperties("r", input);
    input.env.A = "2";
    input.ports.push(443);
    expect(out).toEqual({ env: { A: "1" }, ports: [80] });
  });

  it("preserves nested structures and falsy leaves", () => {
    const input = {
      a: null,
      b: false,
      c: 0,
      d: "",
      e: [1, "x", true, null, { f: [[]] }],
      g: Object.assign(Object.create(null), { h: 1 }),
    };
    expect(sanitizeProperties("r", input)).toEqual({ ...input, g: { h: 1 } });
  });

  it("allows repeated (non-circular) references", () => {
    const shared = { x: 1 };
    expect(sanitizeProperties("r", { a: shared, b: [shared, shared] })).toEqual({
      a: { x: 1 },
      b: [{ x: 1 }, { x: 1 }],
    });
  });

  it("drops object keys whose value is undefined", () => {
    expect(sanitizeProperties("r", { a: undefined, b: { c: undefined, d: 1 } })).toEqual({ b: { d: 1 } });
  });

  it("rejects undefined and holes inside arrays", () => {
    expectDexError(() => sanitizeProperties("r", { ports: [80, undefined] }), notSerializable("ports[1]", "undefined"));
    expectDexError(() => sanitizeProperties("r", { ports: [80, , 1] }), notSerializable("ports[1]", "undefined"));
  });

  class Thing {}
  it.each([
    ["function", () => 1, "function"],
    ["symbol", Symbol("s"), "symbol"],
    ["bigint", 1n, "bigint"],
    ["NaN", Number.NaN, "non-finite number"],
    ["Infinity", Number.POSITIVE_INFINITY, "non-finite number"],
    ["Date", new Date(0), "non-plain object"],
    ["Map", new Map(), "non-plain object"],
    ["class instance", new Thing(), "non-plain object"],
  ])("rejects %s with its path", (_label, value, descriptor) => {
    expectDexError(() => sanitizeProperties("r", { env: { POSTGRES_PASSWORD: value } }), notSerializable("env.POSTGRES_PASSWORD", descriptor));
  });

  it("rejects circular references", () => {
    const root: Record<string, unknown> = {};
    root.self = root;
    expectDexError(() => sanitizeProperties("r", root), 'resource "r" property "self" contains a circular reference');

    const list: unknown[] = [];
    list.push(list);
    expectDexError(() => sanitizeProperties("r", { list }), 'resource "r" property "list[0]" contains a circular reference');
  });

  it.each([null, undefined, "x", 1, [], new Date(0)])("rejects non-object input %s", (input) => {
    expectDexError(() => sanitizeProperties("r", input), 'resource "r" properties must be an object');
  });
});
