/** Limits of shed's webhook endpoint . */
export const webhookLimits = { burst: 8, refillPerSecond: 1, maxActive: 4 } as const;

/** BucketState is the guard's state: tokens, in-flight requests, last refill. */
export type BucketState = { tokens: number; active: number; updatedAt: number | null };

/** newBucket returns a guard that has not seen a request yet. */
export function newBucket(): BucketState {
  return { tokens: webhookLimits.burst, active: 0, updatedAt: null };
}

/** Admission is the outcome of one request reaching the guard. */
export type Admission = "accepted" | "no-token" | "too-many-active";

/**
 * acquire refills the bucket up to now (milliseconds), then admits the
 * request if a token is free and fewer than maxActive requests are in
 * flight. A rejected request still advances the refill clock, as in Go.
 */
export function acquire(s: BucketState, now: number): { state: BucketState; result: Admission } {
  const { burst, refillPerSecond, maxActive } = webhookLimits;
  const tokens =
    s.updatedAt === null
      ? burst
      : Math.min(burst, s.tokens + ((now - s.updatedAt) / 1000) * refillPerSecond);
  const next = { ...s, tokens, updatedAt: now };
  if (next.active >= maxActive) return { state: next, result: "too-many-active" };
  if (next.tokens < 1) return { state: next, result: "no-token" };
  return {
    state: { ...next, active: next.active + 1, tokens: next.tokens - 1 },
    result: "accepted",
  };
}

/** release marks one in-flight request as finished. */
export function release(s: BucketState): BucketState {
  return { ...s, active: Math.max(0, s.active - 1) };
}

/** tokensAt is the token count the bucket would hold at now, without admitting anything. */
export function tokensAt(s: BucketState, now: number): number {
  if (s.updatedAt === null) return webhookLimits.burst;
  const earned = ((now - s.updatedAt) / 1000) * webhookLimits.refillPerSecond;
  return Math.min(webhookLimits.burst, s.tokens + earned);
}
