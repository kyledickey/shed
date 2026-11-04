import { Button, Tabs } from "@cloudflare/kumo";
import { XCircleIcon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { errorMessage } from "../../api/client";
import { metricsRanges } from "../../api/metrics";
import type { MetricsRange } from "../../api/types";
import { Skeleton } from "../../components/Misc";
import { Notice } from "../../components/Notice";
import styles from "./MetricsView.module.css";
import { StatStrip, type Stat } from "./StatStrip";

type MetricsViewProps = {
  /** A line of context on the left of the toolbar. */
  intro?: ReactNode;
  range: MetricsRange;
  setRange: (range: MetricsRange) => void;
  /** The loaded series, or undefined while loading or after an error. */
  data?: { start: string; step: number };
  stats: Stat[];
  /** Chart cards, two to a row. */
  charts: ReactNode;
  /** Shown instead of the charts when the range has no samples. */
  empty?: { title: string; description: string };
  error: Error | null;
  retry: () => void;
  retrying: boolean;
};

/**
 * MetricsView lays out a metrics page: a range toolbar, a strip of current
 * values with sparklines, and a grid of charts.
 */
export function MetricsView({
  intro,
  range,
  setRange,
  data,
  stats,
  charts,
  empty,
  error,
  retry,
  retrying,
}: MetricsViewProps) {
  return (
    <div className={styles.view}>
      <div className={styles.toolbar}>
        <span className={styles.intro}>{intro}</span>
        <Tabs
          variant="segmented"
          size="sm"
          value={range}
          onValueChange={(v) => setRange(v as MetricsRange)}
          tabs={metricsRanges.map((r) => ({ value: r.value, label: r.label }))}
        />
      </div>
      {data ? (
        empty ? (
          <Notice title={empty.title}>{empty.description}</Notice>
        ) : (
          <>
            <StatStrip stats={stats} start={data.start} step={data.step} />
            <div className={styles.grid}>{charts}</div>
          </>
        )
      ) : error ? (
        <Notice
          icon={<XCircleIcon size={16} />}
          title="Couldn't load metrics"
          danger
          actions={
            <Button size="sm" onClick={retry} loading={retrying}>
              Retry
            </Button>
          }
        >
          {errorMessage(error)}
        </Notice>
      ) : (
        <>
          <Skeleton height={102} radius={8} />
          <div className={styles.grid}>
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} height={300} radius={8} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
