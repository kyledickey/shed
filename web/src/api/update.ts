import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import type { UpdateStatus } from "./types";

const POLL_MS = 2000;
const IDLE_MS = 10 * 60_000;

/** updateQuery polls quickly while a check or download runs, and rarely otherwise. */
export const updateQuery = queryOptions({
  queryKey: keys.update,
  queryFn: () => api.get<UpdateStatus>("/update"),
  refetchInterval: (q) => {
    const state = q.state.data?.state;
    return state === "checking" || state === "downloading" ? POLL_MS : IDLE_MS;
  },
});

/** useUpdateAction builds a mutation whose UpdateStatus response replaces the cached status. */
function useUpdateAction<V>(call: (variables: V) => Promise<UpdateStatus>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: call,
    onSuccess: (status) => qc.setQueryData(keys.update, status),
  });
}

export const useCheckUpdate = () => useUpdateAction(() => api.post<UpdateStatus>("/update/check"));

export const useDownloadUpdate = () =>
  useUpdateAction(() => api.post<UpdateStatus>("/update/download"));

export const useSetAutoDownload = () =>
  useUpdateAction((autoDownload: boolean) =>
    api.put<UpdateStatus>("/update/settings", { autoDownload }),
  );

/** useInstallUpdate asks shed to swap in the staged binary and restart. */
export const useInstallUpdate = () =>
  useUpdateAction(() => api.post<UpdateStatus>("/update/install"));
