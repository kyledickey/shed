import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { ImportApp, Setup, User } from "./types";

export const meQuery = queryOptions({
  queryKey: keys.me,
  queryFn: () => api.get<User>("/me"),
  staleTime: 5 * 60_000,
});

export const setupQuery = queryOptions({
  queryKey: keys.setup,
  queryFn: () => api.get<Setup>("/setup"),
  staleTime: 60_000,
});

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<void>("/auth/logout"),
    onSuccess: () => qc.clear(),
  });
}

export function useImportApp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (app: ImportApp) => api.post<Setup>("/setup/github/import", app),
    onSuccess: (setup) => qc.setQueryData(keys.setup, setup),
  });
}
