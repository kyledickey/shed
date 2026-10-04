import { useLayoutEffect, useRef, useState, type PointerEvent } from "react";
import type { Tone } from "../tone";
import styles from "./charts.module.css";
import { monotonePath, niceMax, segments } from "./path";

export type Series = {
  key: string;
  label: string;
  tone: Tone;
  values: (number | null)[];
};

type AreaChartProps = {
  series: Series[];
  /** Timestamp of the first sample. */
  start: string;
  /** Seconds between samples. */
  step: number;
  format: (v: number) => string;
  /** Fixed y maximum, e.g. a memory limit. Defaults to a nice data max. */
  max?: number;
  /** A dashed reference line, e.g. the container limit. */
  limit?: { value: number; label: string };
  height?: number;
};

const PAD = { top: 10, right: 52, bottom: 24, left: 2 };

/** AreaChart plots one or more time series as flat tinted areas with a hover crosshair. */
export function AreaChart({
  series,
  start,
  step,
  format,
  max,
  limit,
  height = 200,
}: AreaChartProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [hover, setHover] = useState<number | null>(null);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? 0));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const n = Math.max(...series.map((s) => s.values.length), 2);
  const dataMax = Math.max(0, ...series.flatMap((s) => s.values.filter((v) => v !== null)));
  const yMax = max ?? niceMax(Math.max(dataMax * 1.15, limit?.value ?? 0));
  const innerW = Math.max(0, width - PAD.left - PAD.right);
  const innerH = height - PAD.top - PAD.bottom;
  const x = (i: number) => PAD.left + (i / (n - 1)) * innerW;
  const y = (v: number) => PAD.top + innerH - (Math.min(v, yMax) / yMax) * innerH;
  const base = PAD.top + innerH;

  const t0 = new Date(start).getTime();
  const span = (n - 1) * step;
  const timeAt = (i: number) => new Date(t0 + i * step * 1000);
  const tickFmt = new Intl.DateTimeFormat(
    undefined,
    span > 2 * 86400 ? { month: "short", day: "numeric" } : { hour: "numeric", minute: "2-digit" },
  );
  const hoverFmt = new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
  const xTicks =
    width > 0 ? Array.from({ length: 5 }, (_, k) => Math.round((k / 4) * (n - 1))) : [];

  const onMove = (e: PointerEvent) => {
    const rect = ref.current!.getBoundingClientRect();
    const px = e.clientX - rect.left - PAD.left;
    setHover(Math.max(0, Math.min(n - 1, Math.round((px / innerW) * (n - 1)))));
  };

  const tipLeft = hover !== null ? x(hover) : 0;
  const tipFlip = tipLeft > width - 200;

  return (
    <div className={styles.areaWrap}>
      {series.length > 1 && (
        <div className={styles.legend}>
          {series.map((s) => (
            <span key={s.key} className={styles.legendItem}>
              <span className={styles.swatch} data-tone={s.tone} />
              {s.label}
            </span>
          ))}
        </div>
      )}
      <div
        ref={ref}
        className={styles.area}
        style={{ height }}
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
      >
        {width > 0 && (
          <svg
            width={width}
            height={height}
            role="img"
            aria-label={series.map((s) => s.label).join(", ")}
          >
            {[0, 0.5, 1].map((f) => (
              <g key={f}>
                <line
                  x1={PAD.left}
                  x2={PAD.left + innerW}
                  y1={y(yMax * f)}
                  y2={y(yMax * f)}
                  className={f === 0 ? styles.baseline : styles.grid}
                />
                <text x={width - 4} y={y(yMax * f) + 4} className={styles.yLabel} textAnchor="end">
                  {format(yMax * f)}
                </text>
              </g>
            ))}

            {xTicks.map((i, k) => (
              <text
                key={k}
                x={x(i)}
                y={height - 6}
                className={styles.xLabel}
                textAnchor={k === 0 ? "start" : k === 4 ? "end" : "middle"}
              >
                {tickFmt.format(timeAt(i))}
              </text>
            ))}

            {series.map((s) =>
              segments(s.values, (v, i) => [x(i), y(v)]).map((run, r) => {
                const line = monotonePath(run);
                const area = `${line}L${run.at(-1)![0]},${base}L${run[0]![0]},${base}Z`;
                return (
                  <g key={`${s.key}-${r}`} data-tone={s.tone}>
                    <path d={area} className={styles.areaFill} />
                    <path d={line} className={styles.line} />
                  </g>
                );
              }),
            )}

            {limit && limit.value <= yMax && (
              <g>
                <line
                  x1={PAD.left}
                  x2={PAD.left + innerW}
                  y1={y(limit.value)}
                  y2={y(limit.value)}
                  className={styles.limit}
                />
                <text x={PAD.left + 6} y={y(limit.value) - 6} className={styles.limitLabel}>
                  {limit.label}
                </text>
              </g>
            )}

            {hover !== null && (
              <g>
                <line
                  x1={x(hover)}
                  x2={x(hover)}
                  y1={PAD.top}
                  y2={base}
                  className={styles.crosshair}
                />
                {series.map((s) => {
                  const v = s.values[hover];
                  return v == null ? null : (
                    <circle
                      key={s.key}
                      cx={x(hover)}
                      cy={y(v)}
                      r={4.5}
                      data-tone={s.tone}
                      className={styles.hoverDot}
                    />
                  );
                })}
              </g>
            )}
          </svg>
        )}
        {hover !== null && (
          <div
            className={styles.tooltip}
            style={{
              left: tipLeft,
              transform: `translateX(${tipFlip ? "calc(-100% - 14px)" : "14px"})`,
            }}
          >
            <div className={styles.tooltipTime}>{hoverFmt.format(timeAt(hover))}</div>
            {series.map((s) => {
              const v = s.values[hover];
              return (
                <div key={s.key} className={styles.tooltipRow}>
                  <span className={styles.swatch} data-tone={s.tone} />
                  <span className={styles.tooltipLabel}>{s.label}</span>
                  <span className={styles.tooltipValue}>{v == null ? "—" : format(v)}</span>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
