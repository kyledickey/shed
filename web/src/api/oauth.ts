import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { OAuthGrant, OAuthRequest } from "./types";

/** oauthRequestQuery loads a pending authorization request. It 404s once expired or used. */
export const oauthRequestQuery = (id: string) =>
  queryOptions({
    queryKey: keys.oauthRequest(id),
    queryFn: () => api.get<OAuthRequest>(`/oauth/requests/${encodeURIComponent(id)}`),
    staleTime: Infinity,
    retry: false,
    refetchOnWindowFocus: false,
  });

/** useDecideOAuthRequest approves or denies a request and returns where to send the browser. */
export function useDecideOAuthRequest(id: string) {
  return useMutation({
    mutationFn: (approve: boolean) =>
      api.post<{ redirect: string }>(`/oauth/requests/${encodeURIComponent(id)}`, { approve }),
  });
}

export const oauthGrantsQuery = queryOptions({
  queryKey: keys.oauthGrants,
  queryFn: () => api.get<OAuthGrant[]>("/oauth/grants"),
});

/** useRevokeOAuthGrant revokes a grant and every token issued under it. */
export function useRevokeOAuthGrant() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.delete(`/oauth/grants/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.oauthGrants }),
  });
}
