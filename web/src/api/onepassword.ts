import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { OnePassword } from "./types";

export const onePasswordQuery = queryOptions({
  queryKey: keys.onePassword,
  queryFn: () => api.get<OnePassword>("/onepassword"),
});

// Connecting and disconnecting move every service's variables, so every
// variables query is stale afterwards.
function refresh(qc: QueryClient, status: OnePassword) {
  qc.setQueryData(keys.onePassword, status);
  return qc.invalidateQueries({
    predicate: ({ queryKey }) => queryKey[0] === "services" && queryKey[2] === "variables",
  });
}

/** useConnectOnePassword moves every service's variables into a 1Password vault. */
export function useConnectOnePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (token: string) => api.put<OnePassword>("/onepassword", { token }),
    onSuccess: (status) => refresh(qc, status),
  });
}

/** useDisconnectOnePassword copies every service's variables back to this server. */
export function useDisconnectOnePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.delete<OnePassword>("/onepassword"),
    onSuccess: (status) => refresh(qc, status),
  });
}
