import { beforeAll, describe, expect, it } from "vitest";
import { COMMANDS_GLOBAL, defineCommand, defineDexConfig, env, service, synth, SYNTH_GLOBAL } from "./index.js";
import { stableStringify } from "./synth.js";
import type { CommandEntry } from "./types.js";

const EXPECTED =
  '{"plugins":[],"project":"my-homelab","resources":{"api":{"depends_on":["postgres"],"plugin":"docker","properties":{"image":"nginx:1.27"},"type":"container"},"postgres":{"depends_on":[],"plugin":"docker","properties":{"image":"postgres:15"},"type":"container"}}}';

const g = globalThis as unknown as Record<string, unknown>;

// The default scope is module-level and defineDexConfig is single-shot:
// this file registers the config exactly once.
beforeAll(() => {
  g.__dex_env = { POSTGRES_VERSION: "15" };
  defineDexConfig({ project: "my-homelab" });
  defineCommand("hello", { description: "Say hello", run: () => 0 });
  defineCommand("bare", { run: () => 7 });
  const pg = service("postgres", {
    plugin: "docker",
    properties: { image: `postgres:${env("POSTGRES_VERSION", "16")}` },
  });
  service("api", { plugin: "docker", properties: { image: "nginx:1.27" }, dependsOn: [pg] });
});

describe("@dex/iac default scope", () => {
  it("synthesizes the IR from the module singletons", () => {
    expect(stableStringify(synth())).toBe(EXPECTED);
  });

  it("installs the host entrypoint on globalThis", () => {
    const hostSynth = g[SYNTH_GLOBAL];
    expect(typeof hostSynth).toBe("function");
    expect((hostSynth as () => string)()).toBe(EXPECTED);
  });

  it("installs the command registry entrypoint and ignores commands in synth output", () => {
    const entries = (g[COMMANDS_GLOBAL] as () => CommandEntry[])();
    expect(entries).toEqual([
      { name: "bare", description: "", run: expect.any(Function) },
      { name: "hello", description: "Say hello", run: expect.any(Function) },
    ]);
    // The command registry is separate from resources: synth output is unchanged.
    expect(entries.length).toBeGreaterThan(0);
    expect(stableStringify(synth())).toBe(EXPECTED);
  });
});
