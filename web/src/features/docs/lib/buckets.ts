/** The bucket window of a metrics query. */

/** Every series has this many buckets. */
export const BUCKETS = 180;

export const rangeSeconds = { "1h": 3600, "6h": 21_600, "24h": 86_400, "7d": 604_800 } as const;

export type BucketRange = keyof typeof rangeSeconds;

export type BucketWindow = {
  /** Bucket width in seconds. */
  step: number;
  /** Unix seconds of the first bucket's start. */
  start: number;
  /** Unix seconds where the last bucket ends. */
  end: number;
  /** Whether the window was shifted back because the current bucket had no samples. */
  shifted: boolean;
};

/**
 * bucketWindow returns the window of a metrics query at unix time now. Buckets
 * are aligned to multiples of the width since the epoch. The window ends with
 * the bucket that contains now, unless that bucket has no samples yet; then it
 * ends one bucket earlier.
 */
export function bucketWindow(
  now: number,
  range: BucketRange,
  currentHasSamples: boolean,
): BucketWindow {
  const step = rangeSeconds[range] / BUCKETS;
  const end = (Math.floor(now / step) + 1) * step;
  const shift = currentHasSamples ? 0 : step;
  return {
    step,
    start: end - step * BUCKETS - shift,
    end: end - shift,
    shifted: !currentHasSamples,
  };
}

/** bucketIndex is the epoch-aligned bucket number a sample at unix time t falls in. */
export function bucketIndex(t: number, step: number): number {
  return Math.floor(t / step);
}
