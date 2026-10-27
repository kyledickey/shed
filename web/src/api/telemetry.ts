import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type {
  ServiceTelemetry,
  Telemetry,
  TelemetryComponentName,
  TelemetrySettings,
} from "./types";

const POLL_MS = 3000;
const IDLE_MS = 15_000;

/** settling reports whether the stack is still moving toward its settings. */
export function settling(t: Telemetry): boolean {
  const want = t.settings.enabled ? "running" : "stopped";
  return t.components.some((c) => c.state !== want && c.state !== "failed");
}

/** telemetryQuery polls quickly while components start or stop, and steadily otherwise. */
export const telemetryQuery = queryOptions({
  queryKey: keys.telemetry,
  queryFn: () => api.get<Telemetry>("/telemetry"),
  refetchInterval: (q) => (q.state.data && settling(q.state.data) ? POLL_MS : IDLE_MS),
});

/** useTelemetryAction builds a mutation whose Telemetry response replaces the cached one. */
function useTelemetryAction<V>(call: (variables: V) => Promise<Telemetry>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: call,
    onSuccess: (t) => qc.setQueryData(keys.telemetry, t),
  });
}

export const useSaveTelemetry = () =>
  useTelemetryAction((s: TelemetrySettings) => api.put<Telemetry>("/telemetry", s));

export const useRestartTelemetryComponent = () =>
  useTelemetryAction((name: TelemetryComponentName) =>
    api.post<Telemetry>(`/telemetry/components/${name}/restart`),
  );

/** useDeleteTelemetryData removes the stack's containers and data, then refetches. */
export function useDeleteTelemetryData() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete("/telemetry/data"),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.telemetry }),
  });
}

export const serviceTelemetryQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.serviceTelemetry(serviceId),
    queryFn: () => api.get<ServiceTelemetry>(`/services/${serviceId}/telemetry`),
    refetchInterval: IDLE_MS,
  });

/** useResetDashboard overwrites one of a service's dashboards with its default. */
export function useResetDashboard(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (uid: string) => api.post(`/telemetry/dashboards/${uid}/reset`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.serviceTelemetry(serviceId) }),
  });
}

/** useRestoreDashboards recreates the default dashboards a user deleted. */
export function useRestoreDashboards() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/telemetry/dashboards/restore"),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["services"] }),
  });
}

/** projectFolderUrl links to the Grafana folder of a project. */
export const projectFolderUrl = (grafanaUrl: string, projectId: string) =>
  `${grafanaUrl}dashboards/f/p-${projectId}/`;

/** OVERVIEW_PAD_MS is how much time a deployment's Overview link shows around it. */
const OVERVIEW_PAD_MS = 15 * 60_000;

/**
 * deploymentOverviewUrl links to a service's Overview dashboard around a
 * deployment, or returns null if the dashboard is not listed.
 */
export function deploymentOverviewUrl(
  t: ServiceTelemetry,
  deployment: { createdAt: string; startedAt: string | null; finishedAt: string | null },
  now = Date.now(),
): string | null {
  const overview = t.dashboards.find((d) => d.uid.startsWith("svc-overview-"));
  if (!overview) return null;
  const from = Date.parse(deployment.startedAt ?? deployment.createdAt) - OVERVIEW_PAD_MS;
  const end = deployment.finishedAt ? Date.parse(deployment.finishedAt) + OVERVIEW_PAD_MS : now;
  return `${overview.url}?from=${from}&to=${Math.min(end, now)}`;
}
