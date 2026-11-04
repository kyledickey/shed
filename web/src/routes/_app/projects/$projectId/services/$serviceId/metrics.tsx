import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { isMetricsRange, metricsQuery } from "../../../../../../api/metrics";
import type { Metrics, MetricsRange } from "../../../../../../api/types";
import { HelpTip } from "../../../../../../features/docs/HelpTip";
import { ChartCard } from "../../../../../../features/metrics/ChartCard";
import { MetricsView } from "../../../../../../features/metrics/MetricsView";
import { latest, type Stat } from "../../../../../../features/metrics/StatStrip";
import { ServiceDashboards } from "../../../../../../features/telemetry/ServiceDashboards";
import { formatBytes, formatPercent, formatRate } from "../../../../../../lib/format";

type MetricsSearch = { range?: MetricsRange };

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/metrics")({
  validateSearch: (search: Record<string, unknown>): MetricsSearch =>
    isMetricsRange(search.range) && search.range !== "1h" ? { range: search.range } : {},
  component: MetricsPage,
});

const seriesKeys = ["cpu", "memory", "netRx", "netTx", "diskRead", "diskWrite"] as const;

function hasSamples(m: Metrics): boolean {
  return seriesKeys.some((k) => m[k].some((v) => v !== null));
}

/** sum adds two series sample by sample, keeping gaps where both are missing. */
function sum(a: (number | null)[], b: (number | null)[]): (number | null)[] {
  return a.map((v, i) => (v === null && b[i] == null ? null : (v ?? 0) + (b[i] ?? 0)));
}

function MetricsPage() {
  const { serviceId } = Route.useParams();
  const { range = "1h" } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { data, error, refetch, isRefetching } = useQuery(metricsQuery(serviceId, range));

  const setRange = (next: MetricsRange) =>
    void navigate({ search: next === "1h" ? {} : { range: next }, replace: true });

  return (
    <>
      <MetricsView
        intro={
          <>
            <HelpTip topic="metrics">Resource usage</HelpTip> of the running container.
          </>
        }
        range={range}
        setRange={setRange}
        data={data}
        stats={data ? stats(data) : []}
        charts={data && <Charts metrics={data} />}
        empty={
          data && !hasSamples(data)
            ? {
                title: "No metrics yet",
                description:
                  "shed samples running containers every 10 seconds. Usage shows up here once this service is running.",
              }
            : undefined
        }
        error={error}
        retry={() => void refetch()}
        retrying={isRefetching}
      />
      <ServiceDashboards serviceId={serviceId} />
    </>
  );
}

function stats(m: Metrics): Stat[] {
  const cpu = latest(m.cpu);
  const memory = latest(m.memory);
  const net = sum(m.netRx, m.netTx);
  const disk = sum(m.diskRead, m.diskWrite);
  const netNow = latest(net);
  const diskNow = latest(disk);
  return [
    {
      label: "CPU",
      value: cpu === null ? null : formatPercent(cpu),
      of: m.cpuLimit > 0 ? `of ${m.cpuLimit} vCPU` : undefined,
      values: m.cpu,
    },
    {
      label: "Memory",
      value: memory === null ? null : formatBytes(memory),
      of: m.memoryLimit > 0 ? `of ${formatBytes(m.memoryLimit)}` : undefined,
      values: m.memory,
    },
    {
      label: "Network",
      value: netNow === null ? null : formatRate(netNow),
      of: "in + out",
      values: net,
    },
    {
      label: "Disk I/O",
      value: diskNow === null ? null : formatRate(diskNow),
      of: "read + write",
      values: disk,
    },
  ];
}

function Charts({ metrics: m }: { metrics: Metrics }) {
  return (
    <>
      <ChartCard
        title="CPU"
        series={[{ key: "cpu", label: "CPU", tone: "accent", values: m.cpu }]}
        start={m.start}
        step={m.step}
        format={formatPercent}
      />
      <ChartCard
        title="Memory"
        series={[{ key: "memory", label: "Memory", tone: "sky", values: m.memory }]}
        start={m.start}
        step={m.step}
        format={formatBytes}
        limit={m.memoryLimit > 0 ? { value: m.memoryLimit, label: "Limit" } : undefined}
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
          { key: "read", label: "Read", tone: "accent", values: m.diskRead },
          { key: "write", label: "Write", tone: "tangerine", values: m.diskWrite },
        ]}
        start={m.start}
        step={m.step}
        format={formatRate}
      />
    </>
  );
}
