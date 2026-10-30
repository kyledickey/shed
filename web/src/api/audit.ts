import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { AuditOutcome, AuditPage, AuditSettings } from "./types";

/** AuditFilters select audit events; "" means any. */
export type AuditFilters = {
  /** A GitHub login or an actor kind. */
  actor: string;
  /** An action or its dotted prefix. */
  action: string;
  service: string;
  outcome: AuditOutcome | "";
};

/** PAGE_SIZE is how many events each "Load more" fetches. */
const PAGE_SIZE = 100;

/** auditPath returns the /audit request for filters, before the event ID before. */
export function auditPath(filters: AuditFilters, before: number | null): string {
  const q = new URLSearchParams({ limit: String(PAGE_SIZE) });
  for (const [k, v] of Object.entries(filters)) {
    if (v) q.set(k, v);
  }
  if (before !== null) q.set("before", String(before));
  return `/audit?${q}`;
}

/** auditQuery pages through the events filters select, newest first. */
export const auditQuery = (filters: AuditFilters) =>
  infiniteQueryOptions({
    queryKey: keys.audit(filters),
    queryFn: ({ pageParam }) => api.get<AuditPage>(auditPath(filters, pageParam)),
    initialPageParam: null as number | null,
    getNextPageParam: (last) => last.next,
  });

export const auditSettingsQuery = queryOptions({
  queryKey: keys.auditSettings,
  queryFn: () => api.get<AuditSettings>("/audit/settings"),
});

/** useSaveAuditSettings saves the retention, then refetches the events, which now include the change. */
export function useSaveAuditSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (s: AuditSettings) => api.put<AuditSettings>("/audit/settings", s),
    onSuccess: (s) => {
      qc.setQueryData(keys.auditSettings, s);
      void qc.invalidateQueries({ queryKey: ["audit", "events"] });
    },
  });
}
