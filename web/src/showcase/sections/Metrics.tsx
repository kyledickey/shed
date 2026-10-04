import { Activity, HardDrive, Network, RotateCcw, Timer } from "lucide-react";
import { useMemo, useState } from "react";
import type { MetricsRange } from "../../api/types";
import { LayerCard } from "../../components/Card";
import { AreaChart, type Series } from "../../components/charts/AreaChart";
import { Meter, UptimeBar, type UptimeDay } from "../../components/charts/Mini";
import { StatTile } from "../../components/charts/StatTile";
import { Segmented } from "../../components/Form";
import { formatBytes, formatPercent, formatRate } from "../../lib/format";
import { sampleMetrics } from "../sample";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

const last = (values: (number | null)[]) => values.findLast((v) => v !== null) ?? 0;

function ChartCard({
  title,
  now,
  unit,
  series,
  start,
  step,
  format,
  max,
  limit,
}: {
  title: string;
  now?: string;
  unit?: string;
  series: Series[];
  start: string;
  step: number;
  format: (v: number) => string;
  max?: number;
  limit?: { value: number; label: string };
}) {
  return (
    <LayerCard
      title={title}
      actions={
        now && (
          <span className={s.chartNow}>
            {now}
            {unit && <small>{unit}</small>}
          </span>
        )
      }
      sheetClassName={s.chartSheet}
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

const uptime: UptimeDay[] = Array.from({ length: 90 }, (_, i) => {
  const d = new Date(Date.now() - (89 - i) * 86400_000).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
  if (i < 6) return { date: d, state: "none", note: "No data" };
  if (i === 41) return { date: d, state: "down", note: "Down 14m" };
  if (i === 63 || i === 64) return { date: d, state: "degraded", note: "Slow responses" };
  return { date: d, state: "up", note: "100% up" };
});

export function Metrics() {
  const [range, setRange] = useState<MetricsRange>("24h");
  const m = useMemo(() => sampleMetrics(range), [range]);
  const mem = last(m.memory);
  const ranges: MetricsRange[] = ["1h", "6h", "24h", "7d"];

  return (
    <>
      <Section
        title="Service metrics"
        description="Every chart has a crosshair tooltip. Limits are dashed; gaps show missing samples."
        actions={
          <Segmented
            label="Range"
            size="sm"
            value={range}
            onChange={setRange}
            options={ranges.map((r) => ({ value: r, label: r }))}
          />
        }
      >
        <Grid min={220}>
          <StatTile
            label="Requests"
            icon={<Activity />}
            value="18.4k"
            unit="/ day"
            delta={{ label: "9%", up: true, good: true }}
            spark={m.netRx.slice(-40)}
            tone="accent"
          />
          <StatTile
            label="Error rate"
            icon={<Network />}
            value="0.21"
            unit="%"
            delta={{ label: "0.05", up: false, good: true }}
            spark={m.diskRead.slice(-40)}
            tone="accent"
          />
          <StatTile
            label="p95 latency"
            icon={<Timer />}
            value="84"
            unit="ms"
            delta={{ label: "6ms", up: true, good: false }}
            spark={m.diskWrite.slice(-40)}
            tone="accent"
          />
          <StatTile
            label="Restarts"
            icon={<RotateCcw />}
            value="0"
            unit="this week"
            footer="Last restart 9 days ago (deploy)"
            tone="accent"
          />
        </Grid>

        <Grid min={440}>
          <ChartCard
            title="CPU"
            now={formatPercent(last(m.cpu))}
            unit={`of ${m.cpuLimit} vCPU`}
            series={[{ key: "cpu", label: "CPU", tone: "accent", values: m.cpu }]}
            start={m.start}
            step={m.step}
            format={formatPercent}
            max={100}
          />
          <ChartCard
            title="Memory"
            now={formatBytes(mem)}
            unit={`of ${formatBytes(m.memoryLimit)}`}
            series={[{ key: "mem", label: "Memory", tone: "sky", values: m.memory }]}
            start={m.start}
            step={m.step}
            format={formatBytes}
            limit={{ value: m.memoryLimit * 0.9, label: "90% of limit" }}
          />
          <ChartCard
            title="Network"
            series={[
              { key: "rx", label: "Inbound", tone: "accent", values: m.netRx },
              { key: "tx", label: "Outbound", tone: "sky", values: m.netTx },
            ]}
            start={m.start}
            step={m.step}
            format={formatRate}
          />
          <ChartCard
            title="Disk I/O"
            series={[
              { key: "r", label: "Read", tone: "accent", values: m.diskRead },
              { key: "w", label: "Write", tone: "tangerine", values: m.diskWrite },
            ]}
            start={m.start}
            step={m.step}
            format={formatRate}
          />
        </Grid>
      </Section>

      <Grid min={340}>
        <Specimen label="Volumes">
          <div className={s.stack}>
            {[
              { name: "/var/lib/postgresql/data", used: 2.1, size: 10 },
              { name: "/data (redis)", used: 0.8, size: 1 },
              { name: "/app/uploads", used: 4.6, size: 5 },
            ].map((v) => (
              <div key={v.name} className={s.meterRow}>
                <div className={s.meterHead}>
                  <span style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
                    <HardDrive size={12} /> <code>{v.name}</code>
                  </span>
                  <span>
                    {v.used} / {v.size} GB
                  </span>
                </div>
                <Meter value={v.used / v.size} />
              </div>
            ))}
          </div>
        </Specimen>
        <Specimen label="Uptime · last 90 days">
          <UptimeBar days={uptime} />
          <div className={s.uptimeFoot}>
            <span>90 days ago</span>
            <span>99.98% uptime</span>
            <span>Today</span>
          </div>
        </Specimen>
      </Grid>
    </>
  );
}
