import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { Metrics, MetricsRange } from "./types";

export const metricsRanges: { value: MetricsRange; label: string }[] = [
  { value: "1h", label: "1h" },
  { value: "6h", label: "6h" },
  { value: "24h", label: "24h" },
  { value: "7d", label: "7d" },
];

/** isMetricsRange reports whether v is a range the metrics endpoint accepts. */
export function isMetricsRange(v: unknown): v is MetricsRange {
  return metricsRanges.some((r) => r.value === v);
}

/**
 * metricsQuery fetches a service's resource usage. It keeps the previous
 * range on screen while a new one loads and refetches about once per sample.
 */
export const metricsQuery = (serviceId: string, range: MetricsRange) =>
  queryOptions({
    queryKey: keys.metrics(serviceId, range),
    queryFn: () => api.get<Metrics>(`/services/${serviceId}/metrics?range=${range}`),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => Math.min(Math.max((q.state.data?.step ?? 10) * 1000, 10_000), 60_000),
  });
