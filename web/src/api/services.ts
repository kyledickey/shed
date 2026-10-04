import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { Domain, NewService, Service, ServicePatch, Variables, Volume } from "./types";

export const serviceQuery = (id: string) =>
  queryOptions({
    queryKey: keys.service(id),
    queryFn: () => api.get<Service>(`/services/${id}`),
    refetchInterval: (q) => (q.state.data?.status === "deploying" ? 3000 : 15_000),
  });

export const variablesQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.variables(serviceId),
    queryFn: () => api.get<Variables>(`/services/${serviceId}/variables`),
  });

function refreshService(qc: QueryClient, service: Service) {
  qc.setQueryData(keys.service(service.id), service);
  return qc.invalidateQueries({ queryKey: keys.project(service.projectId) });
}

export function useCreateService(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: NewService) => api.post<Service>(`/projects/${projectId}/services`, body),
    onSuccess: (service) => {
      qc.setQueryData(keys.service(service.id), service);
      return qc.invalidateQueries({ queryKey: keys.projects });
    },
  });
}

export function useUpdateService(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (patch: ServicePatch) => api.patch<Service>(`/services/${id}`, patch),
    onSuccess: (service) => refreshService(qc, service),
  });
}

export function useDeleteService(service: Service) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete(`/services/${service.id}`),
    onSuccess: () => {
      qc.removeQueries({ queryKey: keys.service(service.id) });
      return qc.invalidateQueries({ queryKey: keys.projects });
    },
  });
}

export function useSaveVariables(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (vars: Variables) => api.put<Variables>(`/services/${serviceId}/variables`, vars),
    onSuccess: (vars) => qc.setQueryData(keys.variables(serviceId), vars),
  });
}

function useServiceChild<TArgs, TResult>(serviceId: string, fn: (args: TArgs) => Promise<TResult>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.service(serviceId), exact: true }),
  });
}

export const useAddDomain = (serviceId: string) =>
  useServiceChild(serviceId, (host?: string) =>
    api.post<Domain>(`/services/${serviceId}/domains`, host ? { host } : {}),
  );

export const useDeleteDomain = (serviceId: string) =>
  useServiceChild(serviceId, (domainId: string) => api.delete(`/domains/${domainId}`));

export const useAddVolume = (serviceId: string) =>
  useServiceChild(serviceId, (mountPath: string) =>
    api.post<Volume>(`/services/${serviceId}/volumes`, { mountPath }),
  );

export const useDeleteVolume = (serviceId: string) =>
  useServiceChild(serviceId, (volumeId: string) => api.delete(`/volumes/${volumeId}`));
