import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { OnePassword, OnePasswordVault } from "./types";

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

/** noVaultsMessage explains what to do when a service account can't see any vault. */
export const noVaultsMessage =
  "This service account can't access any vaults. On 1Password.com, create a service account with read and write access to the vault you want shed to use.";

/** ConnectPlan is what to do after listing the vaults a token can access. */
export type ConnectPlan =
  | { kind: "none" }
  | { kind: "connect"; vault: string }
  | { kind: "choose"; vaults: OnePasswordVault[]; vault: string };

/** planConnect connects straight away with a single vault and asks to choose among several. */
export function planConnect(vaults: OnePasswordVault[]): ConnectPlan {
  const [first, second] = vaults;
  if (!first) return { kind: "none" };
  if (!second) return { kind: "connect", vault: first.id };
  return { kind: "choose", vaults, vault: first.id };
}

/** useOnePasswordVaults lists the vaults a service account token can access. */
export function useOnePasswordVaults() {
  return useMutation({
    mutationFn: (token: string) => api.post<OnePasswordVault[]>("/onepassword/vaults", { token }),
  });
}

/** useConnectOnePassword moves every service's variables into a 1Password vault. */
export function useConnectOnePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { token: string; vault?: string }) =>
      api.put<OnePassword>("/onepassword", body),
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
