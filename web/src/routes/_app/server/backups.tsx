import { createFileRoute } from "@tanstack/react-router";
import { backupSettingsQuery, systemBackupsQuery } from "../../../api/backups";
import { SystemBackups } from "../../../features/backups/SystemBackups";

export const Route = createFileRoute("/_app/server/backups")({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(backupSettingsQuery),
      queryClient.ensureQueryData(systemBackupsQuery),
    ]),
  component: SystemBackups,
});
