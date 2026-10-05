import { describe, expect, it } from "vite-plus/test";
import { isBuildKey, secretMounts } from "./buildPlan";

describe("isBuildKey", () => {
  it.each([
    ["DATABASE_URL", true],
    ["_x1", true],
    ["1ABC", false],
    ["my-key", false],
    ["", false],
  ])("%s", (key, want) => {
    expect(isBuildKey(key)).toBe(want);
  });
});

describe("secretMounts", () => {
  it("mounts and exports each key, sorted", () => {
    expect(secretMounts(["B", "A"], "npm run build")).toBe(
      [
        "RUN --mount=type=secret,id=A \\",
        "    --mount=type=secret,id=B \\",
        '    A="$(cat /run/secrets/A)" \\',
        '    B="$(cat /run/secrets/B)" \\',
        "    npm run build",
      ].join("\n"),
    );
  });
  it("drops invalid names and handles none", () => {
    expect(secretMounts(["bad-key"], "make")).toBe("RUN make");
  });
});
