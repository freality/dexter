import { afterEach, describe, expect, it } from "vitest";
import { HOST_ENV_GLOBAL } from "./env.js";
import { DexError, type DexErrorCode } from "./errors.js";
import { createDexScope } from "./scope.js";
import { stableStringify } from "./synth.js";

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

const g = globalThis as unknown as Record<string, unknown>;

describe("createDexScope", () => {
  afterEach(() => {
    delete g[HOST_ENV_GLOBAL];
  });

  it("isolates resources between scopes", () => {
    const a = createDexScope();
    const b = createDexScope();
    a.defineDexConfig({ project: "a" });
    b.defineDexConfig({ project: "b" });
    a.service("x", { plugin: "docker", properties: {} });
    b.service("x", { plugin: "docker", properties: {} });
    expect(Object.keys(a.synth().resources)).toEqual(["x"]);
    expect(Object.keys(b.synth().resources)).toEqual(["x"]);
  });

  it("returns a frozen copy of the config and rejects a second call", () => {
    const scope = createDexScope();
    const input = { project: "lab" };
    const config = scope.defineDexConfig(input);
    expect(config).toEqual(input);
    expect(config).not.toBe(input);
    expect(Object.isFrozen(config)).toBe(true);
    expectDexError(
      () => scope.defineDexConfig({ project: "again" }),
      "DUPLICATE_CONFIG",
      "defineDexConfig() was called more than once",
    );
  });

  it("exposes secret() which returns a frozen SecretRef", () => {
    const { secret: scopeSecret } = createDexScope();
    const ref = scopeSecret("API_TOKEN");
    expect(ref).toEqual({ $dex_type: "secret", provider: "env", key: "API_TOKEN" });
    expect(Object.isFrozen(ref)).toBe(true);
  });

  it("registers commands behind defineCommand, listed in name order", () => {
    const scope = createDexScope();
    expect(scope.commands.names()).toEqual([]);
    scope.defineCommand("hello", { description: "Say hello", run: (ctx) => ctx.args.length });
    scope.defineCommand("z", { run: () => 0 });
    expect(scope.commands.names()).toEqual(["hello", "z"]);
    const cmd = scope.commands.get("hello");
    expect(cmd?.description).toBe("Say hello");
    expect(cmd?.run({ args: ["a", "b"] } as never)).toBe(2);
  });

  it("isolates commands between scopes, and synth ignores them", () => {
    const a = createDexScope();
    const b = createDexScope();
    a.defineCommand("x", { run: () => 0 });
    b.defineCommand("x", { run: () => 1 });
    expect(a.commands.get("x")?.run({} as never)).toBe(0);
    expect(b.commands.get("x")?.run({} as never)).toBe(1);
    a.defineDexConfig({ project: "a" });
    expect(a.synth()).toEqual({ project: "a", resources: {} });
  });

  it("refuses to synth before a config is defined", () => {
    expectDexError(
      () => createDexScope().synth(),
      "MISSING_CONFIG",
      "no dex config defined: call defineDexConfig() in dex.config.ts",
    );
  });

  it("resolves env through an injected source", () => {
    const scope = createDexScope({ envSource: { get: (key) => (key === "A" ? "injected" : undefined) } });
    expect(scope.env("A")).toBe("injected");
    expect(scope.env("B", "fb")).toBe("fb");
  });

  it("defaults to the host env global", () => {
    g[HOST_ENV_GLOBAL] = { A: "host" };
    expect(createDexScope().env("A")).toBe("host");
  });

  it("builds volume and network resources and serializes deterministically", () => {
    const { defineDexConfig, network, service, synth, synthJSON, volume } = createDexScope();
    defineDexConfig({ project: "lab" });
    const net = network("main", { plugin: "docker", properties: {} });
    const vol = volume("data", { plugin: "docker", properties: {} });
    service("db", { plugin: "docker", properties: {}, dependsOn: [net, vol] });
    const ir = synth();
    expect(ir.resources.main?.type).toBe("network");
    expect(ir.resources.data?.type).toBe("volume");
    expect(ir.resources.db?.depends_on).toEqual(["data", "main"]);
    expect(synthJSON()).toBe(stableStringify(ir));
  });
});
