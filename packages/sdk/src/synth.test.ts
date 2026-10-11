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
  it("emits the SPEC IR shape with sorted keys, depends_on, and plugins", () => {
    const ir = buildIR({ project: "lab", plugins: ["github.com/freality/dexter@v1.0.0"] }, [node("web", ["db"]), node("db")]);
    expect(Object.keys(ir.resources)).toEqual(["db", "web"]);
    expect(ir).toEqual({
      project: "lab",
      plugins: ["github.com/freality/dexter@v1.0.0"],
      resources: {
        db: { type: "container", plugin: "docker", properties: { image: "db:1" }, depends_on: [] },
        web: { type: "container", plugin: "docker", properties: { image: "web:1" }, depends_on: ["db"] },
      },
    });
  });

  it("defaults plugins to [] when omitted", () => {
    const ir = buildIR({ project: "lab" }, []);
    expect(ir.plugins).toEqual([]);
  });

  it("deduplicates plugin specs order-preserving", () => {
    const ir = buildIR(
      { project: "lab", plugins: ["github.com/a/b", "github.com/c/d", "github.com/a/b"] },
      [],
    );
    expect(ir.plugins).toEqual(["github.com/a/b", "github.com/c/d"]);
  });

  it.each([
    [""],
    [1],
    [undefined],
  ])("rejects project %s", (project) => {
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

  describe("plugins validation", () => {
    it.each([
      ["not-a-url"],
      ["github.com/"],
      ["github.com//repo"],
      ["github.com/owner/"],
      ["http://github.com/a/b"],
      ["github.com/a/b@tag@x"],
    ])("rejects malformed plugin spec %s", (spec) => {
      expectDexError(
        () => buildIR({ project: "lab", plugins: [spec] }, []),
        "INVALID_PLUGIN_SPEC",
        'invalid plugin spec "' + spec + '": must match github.com/<owner>/<repo>[@tag]',
      );
    });

    it.each([
      ["github.com/owner/repo"],
      ["github.com/owner/repo-name"],
      ["github.com/owner/repo.name"],
      ["github.com/owner/repo_name"],
      ["github.com/owner/repo@v1.0.0"],
      ["github.com/owner/repo@latest"],
      ["github.com/owner/repo@1.2.3-beta"],
    ])("accepts valid plugin spec %s", (spec) => {
      const ir = buildIR({ project: "lab", plugins: [spec] }, []);
      expect(ir.plugins).toEqual([spec]);
    });

    it("rejects plugins that is not an array", () => {
      expectDexError(
        () => buildIR({ project: "lab", plugins: "nope" } as unknown as DexConfig, []),
        "INVALID_PLUGIN_SPEC",
        "plugins must be an array of strings",
      );
    });
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