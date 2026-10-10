import { describe, expect, it } from "vitest";
import { SDK_NAME } from "./index.js";

describe("@dex/iac tooling", () => {
  it("compiles and runs under vitest", () => {
    expect(SDK_NAME).toBe("@dex/iac");
  });
});
