import { useEffect, useReducer, useRef, useState, type CSSProperties } from "react";
import { Button } from "../../../../components/Button";
import { Segmented } from "../../../../components/Form";
import {
  acquire,
  newBucket,
  release,
  tokensAt,
  webhookLimits,
  type Admission,
  type BucketState,
} from "../../lib/tokenBucket";
import { Demo } from "../../kit";
import styles from "./security.module.css";

type Entry = { n: number; result: Admission };
type State = { bucket: BucketState; log: Entry[]; count: number };
type Action =
  | { type: "send"; times: number; at: number }
  | { type: "release" }
  | { type: "set"; state: State }
  | { type: "reset" };

const initial: State = { bucket: newBucket(), log: [], count: 0 };

function reducer(s: State, a: Action): State {
  switch (a.type) {
    case "send": {
      let bucket = s.bucket;
      let count = s.count;
      const added: Entry[] = [];
      for (let i = 0; i < a.times; i++) {
        const r = acquire(bucket, a.at);
        bucket = r.state;
        added.push({ n: ++count, result: r.result });
      }
      return { bucket, count, log: [...added.reverse(), ...s.log].slice(0, 10) };
    }
    case "release":
      return { ...s, bucket: release(s.bucket) };
    case "set":
      return a.state;
    case "reset":
      return initial;
  }
}

const outcome: Record<Admission, string> = {
  accepted: "accepted, handler runs",
  "no-token": "429 webhook capacity exceeded (no token)",
  "too-many-active": "429 webhook capacity exceeded (4 in flight)",
};

/** TokenBucketSim replays shed's webhook guard locally. It sends nothing over the network. */
export function TokenBucketSim() {
  const [state, dispatch] = useReducer(reducer, initial);
  const [hold, setHold] = useState("1000");
  const [now, setNow] = useState(() => performance.now());

  useEffect(() => {
    const id = setInterval(() => setNow(performance.now()), 100);
    return () => clearInterval(id);
  }, []);

  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const clearTimers = () => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
  };
  useEffect(() => clearTimers, []);

  const tokens = tokensAt(state.bucket, now);
  const send = (times: number) => {
    // Run the reducer here to learn how many requests were admitted, since
    // each one frees its slot when its simulated handler finishes.
    const next = reducer(state, { type: "send", times, at: performance.now() });
    const admitted = next.log.slice(0, times).filter((e) => e.result === "accepted").length;
    dispatch({ type: "set", state: next });
    for (let i = 0; i < admitted; i++) {
      timers.current.push(setTimeout(() => dispatch({ type: "release" }), Number(hold)));
    }
  };

  return (
    <Demo title="Webhook guard simulator">
      <div className={styles.stack}>
        <div className={styles.row}>
          <Button size="sm" onClick={() => send(1)}>
            Send 1
          </Button>
          <Button size="sm" onClick={() => send(10)}>
            Send 10 at once
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              clearTimers();
              dispatch({ type: "reset" });
            }}
          >
            Reset
          </Button>
          <Segmented
            label="How long a handler takes"
            size="sm"
            value={hold}
            onChange={setHold}
            options={[
              { value: "100", label: "100 ms" },
              { value: "1000", label: "1 s" },
              { value: "5000", label: "5 s" },
            ]}
          />
        </div>
        <div className={styles.meterRow}>
          <span>
            tokens {tokens.toFixed(1)} / {webhookLimits.burst}
          </span>
          <span className={styles.meter}>
            <span
              className={styles.meterFill}
              style={{ "--fill": tokens / webhookLimits.burst } as CSSProperties}
            />
          </span>
          <span>
            in flight {state.bucket.active} / {webhookLimits.maxActive}
          </span>
        </div>
        <ul className={styles.log} aria-label="Simulated deliveries, newest first">
          {state.log.length === 0 && <li className={styles.muted}>No deliveries yet.</li>}
          {state.log.map((e) => (
            <li key={e.n}>
              <span className={styles.muted}>#{e.n}</span>
              <span className={e.result === "accepted" ? styles.ok : styles.reject}>
                {outcome[e.result]}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </Demo>
  );
}
