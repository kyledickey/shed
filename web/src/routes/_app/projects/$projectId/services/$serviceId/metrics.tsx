import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { ChartArea, CircleX } from "lucide-react";
import { errorMessage } from "../../../../../../api/client";
import { isMetricsRange, metricsQuery, metricsRanges } from "../../../../../../api/metrics";
import type { Metrics, MetricsRange } from "../../../../../../api/types";
import { Button } from "../../../../../../components/Button";
import { Segmented } from "../../../../../../components/Form";
import { Grid, Section } from "../../../../../../components/Layout";
import { Callout, EmptyState, Skeleton } from "../../../../../../components/Misc";
import { ChartCard, ChartValue } from "../../../../../../features/metrics/ChartCard";
import { formatBytes, formatPercent, formatRate } from "../../../../../../lib/format";
import { HelpTip } from "../../../../../../features/docs/HelpTip";

type MetricsSearch = { range?: MetricsRange };

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/metrics")({
  validateSearch: (search: Record<string, unknown>): MetricsSearch =>
    isMetricsRange(search.range) && search.range !== "1h" ? { range: search.range } : {},
  component: MetricsPage,
});

const latest = (values: (number | null)[]) => values.findLast((v) => v !== null) ?? null;

const seriesKeys = ["cpu", "memory", "netRx", "netTx", "diskRead", "diskWrite"] as const;

function hasSamples(m: Metrics): boolean {
  return seriesKeys.some((k) => m[k].some((v) => v !== null));
}

function MetricsPage() {
  const { serviceId } = Route.useParams();
  const { range = "1h" } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { data, error, refetch, isRefetching } = useQuery(metricsQuery(serviceId, range));

  const setRange = (next: MetricsRange) =>
    void navigate({ search: next === "1h" ? {} : { range: next }, replace: true });

  return (
    <Section
      title={
        <>
          Metrics <HelpTip topic="metrics" />
        </>
      }
      description="Resource usage of the running container."
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
        hasSamples(data) ? (
          <Charts metrics={data} />
        ) : (
          <EmptyState
            icon={<ChartArea />}
            tone="neutral"
            title="No metrics yet"
            description="shed samples running containers every 10 seconds. Usage shows up here once this service is running."
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

function Charts({ metrics: m }: { metrics: Metrics }) {
  const cpu = latest(m.cpu);
  const memory = latest(m.memory);
  const rx = latest(m.netRx);
  const tx = latest(m.netTx);
  const read = latest(m.diskRead);
  const write = latest(m.diskWrite);
  const cpuUnit = m.cpuLimit > 0 ? `of ${m.cpuLimit} vCPU` : undefined;
  const memoryUnit = m.memoryLimit > 0 ? `of ${formatBytes(m.memoryLimit)}` : undefined;

  return (
    <Grid min={440}>
      <ChartCard
        title="CPU"
        now={cpu !== null && <ChartValue value={formatPercent(cpu)} unit={cpuUnit} />}
        series={[{ key: "cpu", label: "CPU", tone: "accent", values: m.cpu }]}
        start={m.start}
        step={m.step}
        format={formatPercent}
        max={m.cpuLimit > 0 ? m.cpuLimit * 100 : undefined}
      />
      <ChartCard
        title="Memory"
        now={memory !== null && <ChartValue value={formatBytes(memory)} unit={memoryUnit} />}
        series={[{ key: "memory", label: "Memory", tone: "sky", values: m.memory }]}
        start={m.start}
        step={m.step}
        format={formatBytes}
        binary
        limit={m.memoryLimit > 0 ? { value: m.memoryLimit, label: "Limit" } : undefined}
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
        binary
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
        binary
      />
    </Grid>
  );
}
