import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import { pagePath } from "./paging";
import { isPending, type Deployment, type DeploymentPage } from "./types";

/** RECENT is how many deployments deploymentsQuery holds. */
const RECENT = 50;

const pollInterval = (list: Deployment[] | undefined) =>
  list?.some((d) => isPending(d.status)) ? 2000 : 15_000;

/**
 * deploymentsQuery is a service's newest deployments: enough for the latest
 * one, the active one, and recent activity, apart from the paged history.
 */
export const deploymentsQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.deployments(serviceId),
    queryFn: async () =>
      (await api.get<DeploymentPage>(pagePath(`/services/${serviceId}/deployments`, null, RECENT)))
        .deployments,
    refetchInterval: (q) => pollInterval(q.state.data),
  });

/** deploymentPageQuery is one page of a service's deployment history, the one after before. */
export const deploymentPageQuery = (serviceId: string, before: string | null) =>
  queryOptions({
    queryKey: keys.deploymentPage(serviceId, before),
    queryFn: () => api.get<DeploymentPage>(pagePath(`/services/${serviceId}/deployments`, before)),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => pollInterval(q.state.data?.deployments),
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
