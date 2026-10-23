import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { OnePassword, Variables } from "./types";

export const onePasswordQuery = queryOptions({
  queryKey: keys.onePassword,
  queryFn: () => api.get<OnePassword>("/onepassword"),
});

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

// keys.onePassword prefixes every Environment listing, so this refreshes those too.
const refresh = (qc: QueryClient) => qc.invalidateQueries({ queryKey: keys.onePassword });

/** useConnectOnePassword saves a service account token; 1Password checks it first. */
export function useConnectOnePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (token: string) => api.put<OnePassword>("/onepassword", { token }),
    onSuccess: () => refresh(qc),
  });
}

export function useDisconnectOnePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete("/onepassword"),
    onSuccess: () => refresh(qc),
  });
}
