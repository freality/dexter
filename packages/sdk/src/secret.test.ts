import { describe, expect, it } from "vitest";
import { DexError, type DexErrorCode } from "./errors.js";
import { sanitizeProperties } from "./properties.js";
import { secret } from "./secret.js";
import { buildIR, stableStringify } from "./synth.js";

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

describe("secret", () => {
  it("returns a frozen SecretRef with the correct shape for a valid key", () => {
    const ref = secret("DB_PASSWORD");
    expect(ref).toEqual({ $dex_type: "secret", provider: "env", key: "DB_PASSWORD" });
    expect(Object.isFrozen(ref)).toBe(true);
    expect(ref.$dex_type).toBe("secret");
    expect(ref.provider).toBe("env");
    expect(ref.key).toBe("DB_PASSWORD");
  });

  it.each([
    ["empty string", "", "empty string"],
    ["number", 42 as unknown as string, "number"],
    ["boolean", true as unknown as string, "boolean"],
    ["null", null as unknown as string, "object"],
    ["undefined", undefined as unknown as string, "undefined"],
    ["object", {} as unknown as string, "object"],
  ])("throws INVALID_SECRET for %s input", (_label, input, expectedType) => {
    expectDexError(
      () => secret(input),
      "INVALID_SECRET",
      `secret() key must be a non-empty string, got ${input === "" ? "empty string" : expectedType}`,
    );
  });

  it("round-trips through sanitizeProperties as a plain object in properties", () => {
    const ref = secret("SECRET_TOKEN");
    const sanitized = sanitizeProperties("r", { token: ref, other: "plain" });
    expect(sanitized).toEqual({ token: { $dex_type: "secret", provider: "env", key: "SECRET_TOKEN" }, other: "plain" });
    // The sanitized object is a plain (unfrozen) clone with the same shape
    expect(Object.isFrozen(sanitized.token)).toBe(false);
    expect(sanitized.token).toEqual({ $dex_type: "secret", provider: "env", key: "SECRET_TOKEN" });
  });

  it("survives buildIR unmodified in resource properties", () => {
    const node = Object.freeze({
      name: "r",
      type: "container",
      plugin: "docker",
      properties: Object.freeze({
        image: "app",
        env: { API_KEY: secret("API_KEY") },
      }),
      dependsOn: Object.freeze([] as readonly string[]),
    });
    const ir = buildIR({ project: "lab" }, [node]);
    expect(ir.resources.r.properties).toEqual({
      image: "app",
      env: { API_KEY: { $dex_type: "secret", provider: "env", key: "API_KEY" } },
    });
  });
});