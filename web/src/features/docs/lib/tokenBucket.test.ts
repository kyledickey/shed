import { describe, expect, it } from "vite-plus/test";
import {
  acquire,
  newBucket,
  release,
  tokensAt,
  type Admission,
  type BucketState,
} from "./tokenBucket";

/** run sends one request per entry at the given times, releasing each right away. */
function run(times: number[], hold = false): Admission[] {
  let s: BucketState = newBucket();
  return times.map((t) => {
    const r = acquire(s, t);
    s = hold ? r.state : release(r.state);
    return r.result;
  });
}

describe("webhook guard", () => {
  it("admits a burst of eight, then rejects", () => {
    const out = run(Array(10).fill(0));
    expect(out.slice(0, 8)).toEqual(Array(8).fill("accepted"));
    expect(out.slice(8)).toEqual(["no-token", "no-token"]);
  });

  it("refills one token per second", () => {
    const times = [...Array(8).fill(0), 500, 1000, 1000];
    const out = run(times);
    expect(out.slice(8)).toEqual(["no-token", "accepted", "no-token"]);
  });

  it("caps in-flight requests at four", () => {
    const out = run(Array(6).fill(0), true);
    expect(out).toEqual([
      "accepted",
      "accepted",
      "accepted",
      "accepted",
      "too-many-active",
      "too-many-active",
    ]);
  });

  it("does not refill past the burst", () => {
    const s = acquire(newBucket(), 0).state;
    expect(tokensAt(s, 60_000)).toBe(8);
  });
});
