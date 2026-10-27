import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type {
  ServiceTelemetry,
  Telemetry,
  TelemetryCaps,
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

/** serviceTelemetryQuery reads a service's caps and polls whether it is dropping records. */
export const serviceTelemetryQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.serviceTelemetry(serviceId),
    queryFn: () => api.get<ServiceTelemetry>(`/services/${serviceId}/telemetry`),
    refetchInterval: IDLE_MS,
  });

/** useSetServiceCaps saves a service's own caps, or with null returns it to the defaults. */
export function useSetServiceCaps(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (caps: TelemetryCaps | null) =>
      api.put<ServiceTelemetry>(`/services/${serviceId}/telemetry`, { caps }),
    onSuccess: (t) => qc.setQueryData(keys.serviceTelemetry(serviceId), t),
  });
}
