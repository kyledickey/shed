import { describe, expect, it } from "vite-plus/test";
import { hasRestarted } from "./restart";

describe("hasRestarted", () => {
  it.each([
    [{ current: "v1.4.0" }, true],
    [{ current: "v1.3.2" }, false],
    [{}, false],
    [null, false],
    ["v1.4.0", false],
  ])("%j", (body, want) => {
    expect(hasRestarted(body, "v1.4.0")).toBe(want);
  });
});
