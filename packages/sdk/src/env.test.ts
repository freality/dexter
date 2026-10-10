import { afterEach, describe, expect, it } from "vitest";
import { createHostEnvSource, envFrom, HOST_ENV_GLOBAL } from "./env.js";
import { DexError } from "./errors.js";

const g = globalThis as unknown as Record<string, unknown>;

describe("createHostEnvSource", () => {
  afterEach(() => {
    delete g[HOST_ENV_GLOBAL];
  });

  it("reads string values from the host global", () => {
    g[HOST_ENV_GLOBAL] = { A: "1" };
    expect(createHostEnvSource().get("A")).toBe("1");
  });

  it("returns undefined for a missing global, non-object global, and non-string value", () => {
    const src = createHostEnvSource();
    expect(src.get("A")).toBeUndefined();
    g[HOST_ENV_GLOBAL] = null;
    expect(src.get("A")).toBeUndefined();
    g[HOST_ENV_GLOBAL] = "nope";
    expect(src.get("A")).toBeUndefined();
    g[HOST_ENV_GLOBAL] = { A: 1 };
    expect(src.get("A")).toBeUndefined();
  });

  it("honours an injected scope", () => {
    const src = createHostEnvSource({ [HOST_ENV_GLOBAL]: { A: "scoped" } });
    expect(src.get("A")).toBe("scoped");
  });

  it("picks up values set after creation", () => {
    const scope: Record<string, unknown> = {};
    const src = createHostEnvSource(scope);
    expect(src.get("A")).toBeUndefined();
    scope[HOST_ENV_GLOBAL] = { A: "late" };
    expect(src.get("A")).toBe("late");
  });
});

describe("envFrom", () => {
  const src = createHostEnvSource({ [HOST_ENV_GLOBAL]: { SET: "v", EMPTY: "" } });

  it("returns the host value over the fallback, including empty strings", () => {
    expect(envFrom(src, "SET", "fb")).toBe("v");
    expect(envFrom(src, "EMPTY", "fb")).toBe("");
  });

  it("uses the fallback when absent", () => {
    expect(envFrom(src, "UNSET", "fb")).toBe("fb");
    expect(envFrom(src, "UNSET", "")).toBe("");
  });

  it("throws MISSING_ENV when absent with no fallback", () => {
    expect(() => envFrom(src, "UNSET")).toThrow(
      new DexError("MISSING_ENV", 'environment variable "UNSET" is not set and no fallback was provided'),
    );
    try {
      envFrom(src, "UNSET");
    } catch (err) {
      expect((err as DexError).code).toBe("MISSING_ENV");
    }
  });
});
