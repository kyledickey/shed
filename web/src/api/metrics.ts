import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api, isApiError } from "./client";
import { keys } from "./keys";
import type { Metrics, MetricsRange } from "./types";

export const metricsRanges: { value: MetricsRange; label: string; seconds: number }[] = [
  { value: "1h", label: "1h", seconds: 3600 },
  { value: "6h", label: "6h", seconds: 6 * 3600 },
  { value: "24h", label: "24h", seconds: 24 * 3600 },
  { value: "7d", label: "7d", seconds: 7 * 24 * 3600 },
];

/** Metrics plus whether they are placeholder data rather than real samples. */
export type MetricsResult = Metrics & { sample: boolean };

export const metricsQuery = (serviceId: string, range: MetricsRange) =>
  queryOptions({
    queryKey: keys.metrics(serviceId, range),
    queryFn: async (): Promise<MetricsResult> => {
      try {
        const metrics = await api.get<Metrics>(`/services/${serviceId}/metrics?range=${range}`);
        return { ...metrics, sample: false };
      } catch (err) {
        // TODO: remove once the metrics endpoint ships.
        if (isApiError(err, 404)) return sampleMetrics(range);
        throw err;
      }
    },
    placeholderData: keepPreviousData,
    refetchInterval: 15_000,
  });

const SAMPLES = 180;

function sampleMetrics(range: MetricsRange): MetricsResult {
  const seconds = metricsRanges.find((r) => r.value === range)!.seconds;
  const step = seconds / SAMPLES;
  const end = Math.floor(Date.now() / 1000 / step) * step;
  const noise = (i: number, seed: number) => {
    const x = Math.sin(i * 12.9898 + seed * 78.233) * 43758.5453;
    return x - Math.floor(x) - 0.5;
  };
  const walk = (base: number, spread: number, seed: number) => {
    let v = base;
    return Array.from({ length: SAMPLES }, (_, i) => {
      v += noise(i, seed) * spread * 0.5 + (base - v) * 0.1;
      return Math.max(0, v + Math.sin((i + seed * 17) / 11) * spread * 0.3);
    });
  };
  const gap = (values: number[]) =>
    values.map((v, i) => (i > SAMPLES * 0.42 && i < SAMPLES * 0.46 ? null : v));
  return {
    range,
    start: new Date((end - step * (SAMPLES - 1)) * 1000).toISOString(),
    step,
    cpuLimit: 2,
    memoryLimit: 512 * 1024 ** 2,
    cpu: gap(walk(18, 14, 1)),
    memory: gap(walk(210 * 1024 ** 2, 30 * 1024 ** 2, 2)),
    netRx: gap(walk(180_000, 120_000, 3)),
    netTx: gap(walk(60_000, 50_000, 4)),
    diskRead: gap(walk(20_000, 30_000, 5)),
    diskWrite: gap(walk(45_000, 40_000, 6)),
    sample: true,
  };
}
