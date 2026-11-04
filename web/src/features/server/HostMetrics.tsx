import { useQuery } from "@tanstack/react-query";
import { hostMetricsQuery } from "../../api/metrics";
import type { HostMetrics, MetricsRange } from "../../api/types";
import { formatBytes, formatPercent, formatRate } from "../../lib/format";
import { ChartCard } from "../metrics/ChartCard";
import { MetricsView } from "../metrics/MetricsView";
import { latest, type Stat } from "../metrics/StatStrip";

/** HostMetrics charts the whole machine's resource usage over a chosen range. */
export function HostMetrics({
  range,
  setRange,
}: {
  range: MetricsRange;
  setRange: (range: MetricsRange) => void;
}) {
  const { data, error, refetch, isRefetching } = useQuery(hostMetricsQuery(range));
  return (
    <MetricsView
      range={range}
      setRange={setRange}
      data={data}
      stats={data ? stats(data) : []}
      charts={data && <Charts metrics={data} />}
      empty={
        data && !data.cpu.some((v) => v !== null)
          ? {
              title: "No metrics yet",
              description: "shed samples the server every 10 seconds. Usage shows up here shortly.",
            }
          : undefined
      }
      error={error}
      retry={() => void refetch()}
      retrying={isRefetching}
    />
  );
}

function stats(m: HostMetrics): Stat[] {
  const cpu = latest(m.cpu);
  const memory = latest(m.memory);
  const disk = latest(m.diskUsed);
  const net = m.netRx.map((v, i) =>
    v === null && m.netTx[i] == null ? null : (v ?? 0) + (m.netTx[i] ?? 0),
  );
  const netNow = latest(net);
  return [
    {
      label: "CPU",
      value: cpu === null ? null : formatPercent(cpu),
      of: `of ${m.cpus} vCPU`,
      values: m.cpu,
    },
    {
      label: "Memory",
      value: memory === null ? null : formatBytes(memory),
      of: `of ${formatBytes(m.memoryTotal)}`,
      values: m.memory,
    },
    {
      label: "Disk space",
      value: disk === null ? null : formatBytes(disk),
      of: `of ${formatBytes(m.diskTotal)}`,
      values: m.diskUsed,
    },
    {
      label: "Network",
      value: netNow === null ? null : formatRate(netNow),
      of: "in + out",
      values: net,
    },
  ];
}

function Charts({ metrics: m }: { metrics: HostMetrics }) {
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
        limit={{ value: m.memoryTotal, label: "Total" }}
      />
      <ChartCard
        title="Disk space"
        series={[{ key: "disk", label: "Used", tone: "tangerine", values: m.diskUsed }]}
        start={m.start}
        step={m.step}
        format={formatBytes}
        limit={{ value: m.diskTotal, label: "Total" }}
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
