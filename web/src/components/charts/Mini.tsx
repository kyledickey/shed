import type { CSSProperties } from "react";
import type { Tone } from "../tone";
import styles from "./charts.module.css";

type HistogramProps<K extends string> = {
  buckets: Record<K, number>[];
  keys: { key: K; tone: Tone }[];
  height?: number;
  /** Highlights the bucket under the pointer and reports it. */
  onHover?: (index: number | null) => void;
};

/** Histogram draws stacked count bars, e.g. log volume by level over time. */
export function Histogram<K extends string>({
  buckets,
  keys,
  height = 44,
  onHover,
}: HistogramProps<K>) {
  const totals = buckets.map((b) => keys.reduce((sum, k) => sum + (b[k.key] ?? 0), 0));
  const max = Math.max(1, ...totals);
  return (
    <div className={styles.histogram} style={{ height }} onPointerLeave={() => onHover?.(null)}>
      {buckets.map((b, i) => (
        <div
          key={i}
          className={styles.histBar}
          onPointerEnter={() => onHover?.(i)}
          style={{ "--fill": `${(totals[i]! / max) * 100}%` } as CSSProperties}
        >
          <div className={styles.histStack}>
            {keys.map((k) =>
              b[k.key] ? (
                <span key={k.key} data-tone={k.tone} style={{ flexGrow: b[k.key] }} />
              ) : null,
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
