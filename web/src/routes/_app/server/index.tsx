import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ChartArea, CircleX } from "lucide-react";
import { errorMessage } from "../../../api/client";
import { hostMetricsQuery, isMetricsRange, metricsRanges } from "../../../api/metrics";
import type { HostMetrics, MetricsRange } from "../../../api/types";
import { Button } from "../../../components/Button";
import { Segmented } from "../../../components/Form";
import { Grid, Section } from "../../../components/Layout";
import { Callout, EmptyState, Skeleton } from "../../../components/Misc";
import { ChartCard, ChartValue } from "../../../features/metrics/ChartCard";
import { formatBytes, formatPercent, formatRate } from "../../../lib/format";
import { HelpTip } from "../../../features/docs/HelpTip";

type ServerSearch = { range?: MetricsRange };

export const Route = createFileRoute("/_app/server/")({
  validateSearch: (search: Record<string, unknown>): ServerSearch =>
    isMetricsRange(search.range) && search.range !== "1h" ? { range: search.range } : {},
  component: ServerPage,
});

const latest = (values: (number | null)[]) => values.findLast((v) => v !== null) ?? null;

function ServerPage() {
  const { range = "1h" } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { data, error, refetch, isRefetching } = useQuery(hostMetricsQuery(range));

  const setRange = (next: MetricsRange) =>
    void navigate({ search: next === "1h" ? {} : { range: next }, replace: true });

  return (
    <Section
      title={
        <>
          Metrics <HelpTip topic="hostMetrics" />
        </>
      }
      description="Resource usage of the whole machine, across all services and everything else."
      actions={
        <Segmented
          label="Range"
          size="sm"
          value={range}
          onChange={setRange}
          options={metricsRanges}
        />
      }
    >
      {data ? (
        data.cpu.some((v) => v !== null) ? (
          <Charts metrics={data} />
        ) : (
          <EmptyState
            icon={<ChartArea />}
            tone="neutral"
            title="No metrics yet"
            description="shed samples the server every 10 seconds. Usage shows up here shortly."
          />
        )
      ) : error ? (
        <Callout
          tone="tomato"
          icon={<CircleX size={16} />}
          title="Couldn't load metrics"
          actions={
            <Button size="sm" onClick={() => void refetch()} loading={isRefetching}>
              Retry
            </Button>
          }
        >
          {errorMessage(error)}
        </Callout>
      ) : (
        <Grid min={440}>
          {Array.from({ length: 4 }, (_, i) => (
            <Skeleton key={i} height={262} radius={18} />
          ))}
        </Grid>
      )}
    </Section>
  );
}

function Charts({ metrics: m }: { metrics: HostMetrics }) {
  const cpu = latest(m.cpu);
  const memory = latest(m.memory);
  const disk = latest(m.diskUsed);
  const rx = latest(m.netRx);
  const tx = latest(m.netTx);
  const read = latest(m.diskRead);
  const write = latest(m.diskWrite);

  return (
    <Grid min={440}>
      <ChartCard
        title="CPU"
        now={cpu !== null && <ChartValue value={formatPercent(cpu)} unit={`of ${m.cpus} vCPU`} />}
        series={[{ key: "cpu", label: "CPU", tone: "accent", values: m.cpu }]}
        start={m.start}
        step={m.step}
        format={formatPercent}
        max={m.cpus * 100}
      />
      <ChartCard
        title="Memory"
        now={
          memory !== null && (
            <ChartValue value={formatBytes(memory)} unit={`of ${formatBytes(m.memoryTotal)}`} />
          )
        }
        series={[{ key: "memory", label: "Memory", tone: "sky", values: m.memory }]}
        start={m.start}
        step={m.step}
        format={formatBytes}
        limit={{ value: m.memoryTotal, label: "Total" }}
      />
      <ChartCard
        title="Disk space"
        now={
          disk !== null && (
            <ChartValue value={formatBytes(disk)} unit={`of ${formatBytes(m.diskTotal)}`} />
          )
        }
        series={[{ key: "disk", label: "Used", tone: "tangerine", values: m.diskUsed }]}
        start={m.start}
        step={m.step}
        format={formatBytes}
        limit={{ value: m.diskTotal, label: "Total" }}
      />
      <ChartCard
        title="Network"
        now={
          (rx !== null || tx !== null) && (
            <>
              {rx !== null && <ChartValue value={formatRate(rx)} unit="in" />}
              {tx !== null && <ChartValue value={formatRate(tx)} unit="out" />}
            </>
          )
        }
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
        now={
          (read !== null || write !== null) && (
            <>
              {read !== null && <ChartValue value={formatRate(read)} unit="read" />}
              {write !== null && <ChartValue value={formatRate(write)} unit="write" />}
            </>
          )
        }
        series={[
          { key: "read", label: "Read", tone: "accent", values: m.diskRead },
          { key: "write", label: "Write", tone: "tangerine", values: m.diskWrite },
        ]}
        start={m.start}
        step={m.step}
        format={formatRate}
      />
    </Grid>
  );
}
