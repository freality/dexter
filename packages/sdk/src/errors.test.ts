import { describe, expect, it } from "vitest";
import { DexError } from "./errors.js";

describe("DexError", () => {
  it("carries code, name, and message and is an Error", () => {
    const err = new DexError("MISSING_ENV", "boom");
    expect(err.code).toBe("MISSING_ENV");
    expect(err.name).toBe("DexError");
    expect(err.message).toBe("boom");
    expect(err).toBeInstanceOf(DexError);
    expect(err).toBeInstanceOf(Error);
  });
});
