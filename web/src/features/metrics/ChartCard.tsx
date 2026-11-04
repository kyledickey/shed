import { LayerCard, TimeseriesChart } from "@cloudflare/kumo";
import type { CSSProperties } from "react";
import { echarts, quickIntro } from "../../components/charts/echarts";
import { axisTime, chartColors, toPoints, type Series } from "../../components/charts/series";
import { useDarkMode } from "../../lib/theme";
import styles from "./ChartCard.module.css";

type ChartCardProps = {
  title: string;
  series: Series[];
  start: string;
  /** Seconds between samples. */
  step: number;
  format: (v: number) => string;
  /** A ceiling drawn as a dashed line, e.g. a memory limit. */
  limit?: { value: number; label: string };
  height?: number;
};

/**
 * ChartCard is a titled time-series chart. A legend above the plot names
 * each series with its current value.
 */
export function ChartCard({
  title,
  series,
  start,
  step,
  format,
  limit,
  height = 220,
}: ChartCardProps) {
  const dark = useDarkMode();
  return (
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>{title}</LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        <ul className={styles.legend}>
          {series.map((s) => {
            const v = s.values.findLast((x) => x !== null);
            return (
              <li key={s.key}>
                <span
                  className={styles.swatch}
                  style={{ "--swatch": chartColors[s.tone] } as CSSProperties}
                />
                {s.label}
                <span className={styles.legendValue}>{v == null ? "–" : format(v)}</span>
              </li>
            );
          })}
        </ul>
        <TimeseriesChart
          ref={quickIntro}
          echarts={echarts}
          gradient
          isDarkMode={dark}
          height={height}
          data={series.map((s) => ({
            name: s.label,
            color: chartColors[s.tone],
            data: toPoints(s.values, start, step),
          }))}
          thresholds={
            limit && [{ value: limit.value, label: limit.label, color: chartColors.tomato }]
          }
          xAxisTickFormat={axisTime(step * (series[0]?.values.length ?? 0))}
          yAxisTickFormat={format}
          tooltipValueFormat={format}
          tooltipFollowCursor="x"
        />
      </LayerCard.Primary>
    </LayerCard>
  );
}
