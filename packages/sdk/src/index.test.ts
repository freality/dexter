import { beforeAll, describe, expect, it } from "vitest";
import { defineDexConfig, env, service, synth, SYNTH_GLOBAL } from "./index.js";
import { stableStringify } from "./synth.js";

const EXPECTED =
  '{"project":"my-homelab","resources":{"api":{"depends_on":["postgres"],"plugin":"docker","properties":{"image":"nginx:1.27"},"type":"container"},"postgres":{"depends_on":[],"plugin":"docker","properties":{"image":"postgres:15"},"type":"container"}}}';

const g = globalThis as unknown as Record<string, unknown>;

// The default scope is module-level and defineDexConfig is single-shot:
// this file registers the config exactly once.
beforeAll(() => {
  g.__dex_env = { POSTGRES_VERSION: "15" };
  defineDexConfig({ project: "my-homelab" });
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
});
