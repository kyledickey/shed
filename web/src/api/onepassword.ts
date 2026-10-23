import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import { refreshService } from "./services";
import type { Service, Variables } from "./types";

/**
 * onePasswordVariablesQuery lists the variables of a service's linked
 * Environment. Every fetch is a 1Password read that counts against the
 * service account's rate limit, so results stay fresh for a minute and
 * failures are not retried. The Environment ID is part of the key so linking
 * another one fetches it at once.
 */
export const onePasswordVariablesQuery = (serviceId: string, environment: string) =>
  queryOptions({
    queryKey: keys.onePasswordVariables(serviceId, environment),
    queryFn: () => api.get<Variables>(`/services/${serviceId}/variables/onepassword`),
    staleTime: 60_000,
    retry: false,
  });

/**
 * useLinkOnePassword links a service to an Environment with a service account
 * token; shed reads the Environment with it before saving. A blank token keeps
 * the saved one.
 */
export function useLinkOnePassword(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (link: { environment: string; token?: string }) =>
      api.put<Service>(`/services/${serviceId}/onepassword`, link),
    onSuccess: (service) =>
      Promise.all([
        refreshService(qc, service),
        qc.invalidateQueries({ queryKey: keys.onePasswordVariables(serviceId) }),
      ]),
  });
}

export function useUnlinkOnePassword(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete<Service>(`/services/${serviceId}/onepassword`),
    onSuccess: (service) =>
      Promise.all([
        refreshService(qc, service),
        qc.invalidateQueries({ queryKey: keys.onePasswordVariables(serviceId) }),
      ]),
  });
}
