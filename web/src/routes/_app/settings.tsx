import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { backupSettingsQuery, systemBackupsQuery } from "../../api/backups";
import { Page, Stack } from "../../components/Layout";
import { PageHeader } from "../../components/Shell";
import { EncryptionCard } from "../../features/backups/EncryptionCard";
import { S3Card } from "../../features/backups/S3Card";
import { SystemBackups } from "../../features/backups/SystemBackups";

export const Route = createFileRoute("/_app/settings")({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(backupSettingsQuery),
      queryClient.ensureQueryData(systemBackupsQuery),
    ]),
  component: SettingsPage,
});

function SettingsPage() {
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);
  return (
    <Page>
      <PageHeader
        title="Settings"
        description="Backup storage and encryption for this shed instance."
      />
      <Stack gap={4}>
        <S3Card settings={settings} />
        <EncryptionCard settings={settings} />
        <SystemBackups />
      </Stack>
    </Page>
  );
}
