import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { backupSettingsQuery } from "../../api/backups";
import { Page, Stack } from "../../components/Layout";
import { PageHeader } from "../../components/Shell";
import { EncryptionCard } from "../../features/backups/EncryptionCard";
import { S3Card } from "../../features/backups/S3Card";
import { UpdatesCard } from "../../features/settings/UpdatesCard";

export const Route = createFileRoute("/_app/settings")({
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(backupSettingsQuery),
  component: SettingsPage,
});

function SettingsPage() {
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);
  return (
    <Page>
      <PageHeader
        title="Settings"
        description="Updates, backup storage, and encryption for this shed instance."
      />
      <Stack gap={4}>
        <UpdatesCard />
        <S3Card settings={settings} />
        <EncryptionCard settings={settings} />
      </Stack>
    </Page>
  );
}
