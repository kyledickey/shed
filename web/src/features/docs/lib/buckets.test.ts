import { describe, expect, it } from "vite-plus/test";
import { BUCKETS, bucketIndex, bucketWindow, rangeSeconds, type BucketRange } from "./buckets";

describe("bucketWindow", () => {
  const now = 1_790_000_005;
  const tests: { range: BucketRange; step: number }[] = [
    { range: "1h", step: 20 },
    { range: "6h", step: 120 },
    { range: "24h", step: 480 },
    { range: "7d", step: 3360 },
  ];
  for (const { range, step } of tests) {
    it(`${range} has ${step}s buckets aligned to the epoch`, () => {
      const w = bucketWindow(now, range, true);
      expect(w.step).toBe(step);
      expect(w.start % step).toBe(0);
      expect(w.end - w.start).toBe(rangeSeconds[range]);
      expect(w.end).toBeGreaterThan(now);
      expect(w.end - step).toBeLessThanOrEqual(now);
      expect(w.shifted).toBe(false);
    });

    it(`${range} shifts back one bucket when the current one is empty`, () => {
      const live = bucketWindow(now, range, true);
      const young = bucketWindow(now, range, false);
      expect(young.start).toBe(live.start - step);
      expect(young.end).toBe(live.end - step);
      expect(young.shifted).toBe(true);
      expect((young.end - young.start) / step).toBe(BUCKETS);
    });
  }
});

describe("bucketIndex", () => {
  it("groups samples by epoch-aligned bucket", () => {
    expect(bucketIndex(39, 20)).toBe(1);
    expect(bucketIndex(40, 20)).toBe(2);
    expect(bucketIndex(59, 20)).toBe(2);
  });
});
