import { describe, expect, it } from "vite-plus/test";
import { builderCreateCommand, diskGuard, guardVerdict } from "./builder";

describe("builderCreateCommand", () => {
  it("applies the default limits", () => {
    const cmd = builderCreateCommand(2048, 2);
    expect(cmd).toContain("--driver-opt memory=2147483648");
    expect(cmd).toContain("--driver-opt memory-swap=2147483648");
    expect(cmd).toContain("--driver-opt cpu-quota=200000");
  });
  it("omits unlimited options", () => {
    const cmd = builderCreateCommand(0, 0);
    expect(cmd).not.toContain("memory");
    expect(cmd).not.toContain("cpu-");
  });
  it("floors the CPU quota at 1000", () => {
    expect(builderCreateCommand(0, 0.001)).toContain("cpu-quota=1000");
  });
});

describe("guardVerdict", () => {
  const g = diskGuard(2048);
  const mib = 1024 * 1024;
  it.each([
    [4096 * mib, "ok"],
    [2048 * mib, "ok"],
    [2047 * mib, "refuse"],
    [1024 * mib, "refuse"],
    [1023 * mib, "cancel"],
  ])("%i bytes free", (free, want) => {
    expect(guardVerdict(free, g)).toBe(want);
  });
  it("is off at zero", () => {
    expect(guardVerdict(0, diskGuard(0))).toBe("ok");
  });
});
