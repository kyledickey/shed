import type { CSSProperties } from "react";
import { Tooltip } from "../Overlay";
import type { Tone } from "../tone";
import styles from "./charts.module.css";
import { monotonePath, segments } from "./path";

/** Sparkline is an axis-free trend line that stretches to its container. */
export function Sparkline({
  values,
  tone = "accent",
  height = 36,
}: {
  values: (number | null)[];
  tone?: Tone;
  height?: number;
}) {
  const W = 100;
  const nums = values.filter((v): v is number => v !== null);
  const lo = Math.min(...nums);
  const hi = Math.max(...nums);
  const range = hi - lo || 1;
  const runs = segments(values, (v, i) => [
    (i / Math.max(1, values.length - 1)) * W,
    2 + (1 - (v - lo) / range) * (height - 4),
  ]);
  return (
    <svg
      className={styles.spark}
      viewBox={`0 0 ${W} ${height}`}
      preserveAspectRatio="none"
      style={{ height }}
      data-tone={tone}
      aria-hidden
    >
      {runs.map((run, i) => {
        const line = monotonePath(run);
        return (
          <g key={i}>
            <path
              d={`${line}L${run.at(-1)![0]},${height}L${run[0]![0]},${height}Z`}
              className={styles.areaFill}
            />
            <path d={line} className={styles.line} vectorEffect="non-scaling-stroke" />
          </g>
        );
      })}
    </svg>
  );
}

/** Meter is a horizontal usage bar; it warms up as it fills. */
export function Meter({ value, tone }: { value: number; tone?: Tone }) {
  const v = Math.max(0, Math.min(1, value));
  const auto: Tone = v > 0.9 ? "tomato" : v > 0.75 ? "sunflower" : "accent";
  return (
    <div className={styles.meter} data-tone={tone ?? auto}>
      <span style={{ width: `${v * 100}%` }} />
    </div>
  );
}

export type UptimeDay = { date: string; state: "up" | "degraded" | "down" | "none"; note?: string };

const uptimeTones: Record<UptimeDay["state"], Tone> = {
  up: "grass",
  degraded: "sunflower",
  down: "tomato",
  none: "neutral",
};

/** UptimeBar is a row of daily health pills. */
export function UptimeBar({ days }: { days: UptimeDay[] }) {
  return (
    <div className={styles.uptime}>
      {days.map((d) => (
        <Tooltip key={d.date} content={`${d.date} · ${d.note ?? d.state}`}>
          <span className={styles.uptimeDay} data-tone={uptimeTones[d.state]} />
        </Tooltip>
      ))}
    </div>
  );
}

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
