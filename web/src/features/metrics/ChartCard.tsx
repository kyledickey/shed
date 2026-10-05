import type { ReactNode } from "react";
import { LayerCard } from "../../components/Card";
import { AreaChart, type Series } from "../../components/charts/AreaChart";
import styles from "./ChartCard.module.css";

type ChartCardProps = {
  title: string;
  /** Current value, shown in the card header. */
  now?: ReactNode;
  series: Series[];
  start: string;
  step: number;
  format: (v: number) => string;
  max?: number;
  limit?: { value: number; label: string };
};

/** ChartCard is a titled time-series chart with its current value in the header. */
export function ChartCard({ title, now, series, start, step, format, max, limit }: ChartCardProps) {
  return (
    <LayerCard
      title={title}
      actions={now && <span className={styles.now}>{now}</span>}
      sheetClassName={styles.sheet}
    >
      <AreaChart
        series={series}
        start={start}
        step={step}
        format={format}
        max={max}
        limit={limit}
        height={190}
      />
    </LayerCard>
  );
}

/** ChartValue is a header value with a quiet unit or label after it. */
export function ChartValue({ value, unit }: { value: string; unit?: string }) {
  return (
    <span className={styles.value}>
      {value}
      {unit && <small>{unit}</small>}
    </span>
  );
}
