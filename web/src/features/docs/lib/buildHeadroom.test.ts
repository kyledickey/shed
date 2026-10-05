import { describe, expect, it } from "vite-plus/test";
import { headroom } from "./buildHeadroom";

const GiB = 1024 ** 3;
const host = { cpus: 4, memoryTotal: 8 * GiB, diskTotal: 100 * GiB, diskUsed: 50 * GiB };

describe("headroom disk", () => {
  const cases = [
    { name: "plenty", used: 50, min: 2048, want: "ok" },
    { name: "just under the minimum", used: 99, min: 2048, want: "refused" },
    { name: "under half the minimum", used: 99.5, min: 2048, want: "canceled" },
    { name: "check disabled", used: 99.9, min: 0, want: "off" },
  ] as const;
  it.each(cases)("$name", ({ used, min, want }) => {
    const h = headroom(
      { ...host, diskUsed: used * GiB },
      { memoryMB: 2048, cpus: 2, minFreeMB: min },
    );
    expect(h.disk).toBe(want);
  });
});

describe("headroom shares", () => {
  it("computes shares of the host", () => {
    const h = headroom(host, { memoryMB: 2048, cpus: 2, minFreeMB: 2048 });
    expect(h.memoryShare).toBeCloseTo(0.25);
    expect(h.cpuShare).toBe(0.5);
  });
  it("is null when unlimited", () => {
    const h = headroom(host, { memoryMB: 0, cpus: 0, minFreeMB: 0 });
    expect(h.memoryShare).toBeNull();
    expect(h.cpuShare).toBeNull();
  });
});
