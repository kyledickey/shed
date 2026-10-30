import { infiniteQueryOptions } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { LogPage } from "./types";

export const historyRanges = ["1h", "6h", "24h", "7d"] as const;
export type HistoryRange = (typeof historyRanges)[number];

export type HistoryFilters = {
  /** The search box: words, "quoted phrases", field:value. */
  q: string;
  /** "" for any level. */
  level: "" | "debug" | "info" | "warn" | "error";
  range: HistoryRange;
};

const HOURS: Record<HistoryRange, number> = { "1h": 1, "6h": 6, "24h": 24, "7d": 168 };

/** PAGE_SIZE is how many records each "Load older" fetches. */
const PAGE_SIZE = 100;

/** historyPath returns the request for filters, before the cursor `before`, with the range ending at now. */
export function historyPath(
  serviceId: string,
  f: HistoryFilters,
  before: string | null,
  now: Date,
): string {
  const q = new URLSearchParams({ limit: String(PAGE_SIZE) });
  if (f.q) q.set("q", f.q);
  if (f.level) q.set("level", f.level);
  q.set("from", new Date(now.getTime() - HOURS[f.range] * 3_600_000).toISOString());
  if (before) q.set("before", before);
  return `/services/${serviceId}/logs/history?${q}`;
}

/** logHistoryQuery pages through the stored logs of a service, newest first. */
export const logHistoryQuery = (serviceId: string, f: HistoryFilters) =>
  infiniteQueryOptions({
    queryKey: keys.logHistory(serviceId, f),
    queryFn: ({ pageParam }) => api.get<LogPage>(historyPath(serviceId, f, pageParam, new Date())),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.next || undefined,
  });

/** traceHref is the link that opens a trace in Grafana Explore. */
export const traceHref = (traceId: string) => `/api/telemetry/trace/${traceId}`;
