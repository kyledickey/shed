import { Chart } from "@cloudflare/kumo";
import { useLayoutEffect, useRef, useState, type CSSProperties } from "react";
import { echarts } from "../../components/charts/echarts";
import { areaFill, chartColors, toPoints } from "../../components/charts/series";
import { Pill } from "../../components/Pill";
import { useDarkMode } from "../../lib/theme";
import styles from "./StatStrip.module.css";

/** Stat is one cell of a StatStrip. */
export type Stat = {
  label: string;
  /** The current value, or null when there are no samples. */
  value: string | null;
  /** Quiet context after the value, such as "of 2 vCPU". */
  of?: string;
  values: (number | null)[];
};

/** latest returns the newest sample, or null when there is none. */
export const latest = (values: (number | null)[]) => values.findLast((v) => v !== null) ?? null;

/** MIN_CELL is the narrowest a stat gets before the strip drops to fewer columns. */
const MIN_CELL = 220;

/**
 * columns picks the most columns that divide n evenly and keep each cell at
 * least MIN_CELL wide, so every row of the strip is full.
 */
export function columns(n: number, width: number): number {
  for (let d = n; d > 1; d--) if (n % d === 0 && d * MIN_CELL <= width) return d;
  return 1;
}

/**
 * StatStrip is a row of stats divided by hairlines: a label, the current
 * value, and a sparkline of the range along the bottom. Where they don't
 * fit in one row, they wrap into a grid with no empty cells.
 */
export function StatStrip({ stats, start, step }: { stats: Stat[]; start: string; step: number }) {
  const dark = useDarkMode();
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number | null>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? null));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const cols = width === null ? stats.length : columns(stats.length, width);
  return (
    <div ref={ref} className={styles.strip} style={{ "--cols": cols } as CSSProperties}>
      {stats.map((s) => (
        <div key={s.label} className={styles.cell}>
          <span className={styles.label}>{s.label}</span>
          {s.value === null ? (
            <span className={styles.none}>
              <Pill>No data</Pill>
            </span>
          ) : (
            <span className={styles.value}>
              {s.value}
              {s.of && <small>{s.of}</small>}
            </span>
          )}
          <Chart
            echarts={echarts}
            isDarkMode={dark}
            height={36}
            className={styles.spark}
            options={{
              backgroundColor: "transparent",
              animation: false,
              grid: { top: 2, right: 0, bottom: 1, left: 0 },
              xAxis: { type: "time", show: false },
              yAxis: { type: "value", show: false, min: 0 },
              series: [
                {
                  type: "line",
                  // With no samples, a flat line under the "No data" pill.
                  data: toPoints(s.value === null ? s.values.map(() => 0) : s.values, start, step),
                  showSymbol: false,
                  lineStyle: {
                    width: 1.5,
                    color: s.value === null ? chartColors.neutral : chartColors.sky,
                  },
                  ...(s.value !== null && { areaStyle: areaFill(chartColors.sky) }),
                },
              ],
            }}
          />
        </div>
      ))}
    </div>
  );
}
