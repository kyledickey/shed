import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { keys } from "./keys";
import {
  isBackupActive,
  type Backup,
  type BackupPolicy,
  type BackupPolicyInput,
  type BackupSettings,
  type BackupSettingsInput,
  type Restore,
  type ServiceBackups,
  type SystemBackups,
} from "./types";

const POLL_MS = 2000;

/** Polls only while something is queued or running; otherwise the data stays put. */
function pollWhileActive(data: SystemBackups | ServiceBackups | undefined): number | false {
  const restoring = data && "restore" in data && data.restore?.status === "running";
  return restoring || data?.backups.some(isBackupActive) ? POLL_MS : false;
}

export const serviceBackupsQuery = (serviceId: string) =>
  queryOptions({
    queryKey: keys.serviceBackups(serviceId),
    queryFn: () => api.get<ServiceBackups>(`/services/${serviceId}/backups`),
    refetchInterval: (q) => pollWhileActive(q.state.data),
  });

export const systemBackupsQuery = queryOptions({
  queryKey: keys.systemBackups,
  queryFn: () => api.get<SystemBackups>("/backups/system"),
  refetchInterval: (q) => pollWhileActive(q.state.data),
});

export const backupSettingsQuery = queryOptions({
  queryKey: keys.backupSettings,
  queryFn: () => api.get<BackupSettings>("/backups/settings"),
});

/** downloadUrl is the decrypted, still zstd-compressed archive; use it as a plain link. */
export const downloadUrl = (backupId: string) => `/api/backups/${backupId}/download`;

/** Scope picks whose backups an action touches: a service id, or null for shed.db. */
type Scope = string | null;

const scopeKey = (scope: Scope) => (scope ? keys.serviceBackups(scope) : keys.systemBackups);

function refresh(qc: QueryClient, scope: Scope) {
  return qc.invalidateQueries({ queryKey: scopeKey(scope), exact: true });
}

export function useRunBackup(scope: Scope) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<Backup>(scope ? `/services/${scope}/backups` : "/backups/system"),
    onSuccess: () => refresh(qc, scope),
  });
}

export function useSaveBackupPolicy(scope: Scope) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (policy: BackupPolicyInput) =>
      api.put<BackupPolicy>(
        scope ? `/services/${scope}/backups/policy` : "/backups/system/policy",
        policy,
      ),
    onSuccess: (policy) =>
      qc.setQueryData<SystemBackups | ServiceBackups>(scopeKey(scope), (old) =>
        old ? { ...old, policy } : old,
      ),
  });
}

export function useDeleteBackup(scope: Scope) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (backupId: string) => api.delete(`/backups/${backupId}`),
    onSuccess: () => refresh(qc, scope),
  });
}

/** useRestoreBackup restores a service backup. The service is held while it runs. */
export function useRestoreBackup(serviceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (backupId: string) => api.post<Restore>(`/backups/${backupId}/restore`),
    onSuccess: () =>
      Promise.all([
        refresh(qc, serviceId),
        qc.invalidateQueries({ queryKey: keys.service(serviceId), exact: true }),
      ]),
  });
}

export function useSaveBackupSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: BackupSettingsInput) => api.put<BackupSettings>("/backups/settings", input),
    onSuccess: (settings) => qc.setQueryData(keys.backupSettings, settings),
  });
}

/** useTestBackupSettings checks an S3 destination without saving it. */
export function useTestBackupSettings() {
  return useMutation({
    mutationFn: (input: BackupSettingsInput) => api.post<void>("/backups/settings/test", input),
  });
}

/** useRevealBackupKey fetches the age secret key on demand so it never sits in the query cache. */
export function useRevealBackupKey() {
  return useMutation({
    mutationFn: () => api.get<{ identity: string }>("/backups/settings/key"),
  });
}
