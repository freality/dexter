import { describe, expect, it } from "vitest";
import { createCommandRegistry } from "./commands.js";
import { DexError } from "./errors.js";

describe("createCommandRegistry", () => {
  it("registers, looks up, and lists commands in name order", () => {
    const reg = createCommandRegistry();
    const zebra = { description: "z", run: () => 0 };
    const alpha = { run: () => 1 };
    reg.define("zebra", zebra);
    reg.define("alpha", alpha);
    expect(reg.get("zebra")).toBe(zebra);
    expect(reg.get("alpha")).toBe(alpha);
    expect(reg.get("missing")).toBeUndefined();
    expect(reg.names()).toEqual(["alpha", "zebra"]);
  });

  it("rejects duplicate names with DUPLICATE_COMMAND", () => {
    const reg = createCommandRegistry();
    reg.define("dupe", { run: () => 0 });
    let caught: unknown;
    try {
      reg.define("dupe", { run: () => 1 });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(DexError);
    expect((caught as DexError).code).toBe("DUPLICATE_COMMAND");
    expect((caught as DexError).message).toBe('command "dupe" is already defined');
  });
});
