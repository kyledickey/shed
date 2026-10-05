import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import { isPending, type Deployment } from "./types";

export const deploymentsQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.deployments(serviceId),
    queryFn: () => api.get<Deployment[]>(`/services/${serviceId}/deployments`),
    refetchInterval: (q) => (q.state.data?.some((d) => isPending(d.status)) ? 2000 : 15_000),
  });

export function invalidateDeployments(qc: QueryClient, serviceId: string) {
  return Promise.all([
    qc.invalidateQueries({ queryKey: keys.deployments(serviceId) }),
    qc.invalidateQueries({ queryKey: keys.service(serviceId), exact: true }),
    qc.invalidateQueries({ queryKey: keys.projects }),
  ]);
}

function useDeploymentAction<TArg>(serviceId: string, fn: (arg: TArg) => Promise<Deployment>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => invalidateDeployments(qc, serviceId),
  });
}

export const useDeploy = (serviceId: string) =>
  useDeploymentAction(serviceId, () => api.post<Deployment>(`/services/${serviceId}/deployments`));

export const useRedeploy = (serviceId: string) =>
  useDeploymentAction(serviceId, (deploymentId: string) =>
    api.post<Deployment>(`/deployments/${deploymentId}/redeploy`),
  );

export const useCancelDeployment = (serviceId: string) =>
  useDeploymentAction(serviceId, (deploymentId: string) =>
    api.post<Deployment>(`/deployments/${deploymentId}/cancel`),
  );
