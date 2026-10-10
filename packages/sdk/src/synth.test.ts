import { describe, expect, it } from "vitest";
import { DexError, type DexErrorCode } from "./errors.js";
import { buildIR, stableStringify } from "./synth.js";
import type { DexConfig, ResourceNode } from "./types.js";

const node = (name: string, dependsOn: string[] = []): ResourceNode => ({
  name,
  type: "container",
  plugin: "docker",
  properties: { image: `${name}:1` },
  dependsOn,
});

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

describe("buildIR", () => {
  it("emits the SPEC IR shape with sorted keys and depends_on always present", () => {
    const ir = buildIR({ project: "lab" }, [node("web", ["db"]), node("db")]);
    expect(Object.keys(ir.resources)).toEqual(["db", "web"]);
    expect(ir).toEqual({
      project: "lab",
      resources: {
        db: { type: "container", plugin: "docker", properties: { image: "db:1" }, depends_on: [] },
        web: { type: "container", plugin: "docker", properties: { image: "web:1" }, depends_on: ["db"] },
      },
    });
  });

  it.each([[""], [1], [undefined]])("rejects project %s", (project) => {
    expectDexError(
      () => buildIR({ project } as unknown as DexConfig, []),
      "INVALID_PROJECT",
      "invalid project name: must be a non-empty string",
    );
  });

  it("rejects unknown dependency targets", () => {
    expectDexError(
      () => buildIR({ project: "lab" }, [node("web", ["db"])]),
      "INVALID_DEPENDENCY",
      'resource "web" depends on unknown resource "db"',
    );
  });

  it("rejects self dependencies", () => {
    expectDexError(
      () => buildIR({ project: "lab" }, [node("web", ["web"])]),
      "INVALID_DEPENDENCY",
      'resource "web" cannot depend on itself',
    );
  });
});

describe("stableStringify", () => {
  it("sorts nested object keys and preserves array order", () => {
    expect(stableStringify({ b: [3, 1, { z: 1, a: 2 }], a: { d: null, c: true } })).toBe(
      '{"a":{"c":true,"d":null},"b":[3,1,{"a":2,"z":1}]}',
    );
  });

  it("escapes strings and handles scalars", () => {
    expect(stableStringify('a"b\n')).toBe('"a\\"b\\n"');
    expect(stableStringify(null)).toBe("null");
    expect(stableStringify(1.5)).toBe("1.5");
    expect(stableStringify(false)).toBe("false");
  });
});
