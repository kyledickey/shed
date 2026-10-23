import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { backupSettingsQuery } from "../../api/backups";
import { onePasswordQuery } from "../../api/onepassword";
import { Page, Stack } from "../../components/Layout";
import { PageHeader } from "../../components/Shell";
import { EncryptionCard } from "../../features/backups/EncryptionCard";
import { S3Card } from "../../features/backups/S3Card";
import { AgentsCard } from "../../features/settings/AgentsCard";
import { OnePasswordCard } from "../../features/settings/OnePasswordCard";
import { UpdatesCard } from "../../features/settings/UpdatesCard";

export const Route = createFileRoute("/_app/settings")({
  loader: ({ context: { queryClient } }) =>
    Promise.all([
      queryClient.ensureQueryData(backupSettingsQuery),
      queryClient.ensureQueryData(onePasswordQuery),
    ]),
  component: SettingsPage,
});

function SettingsPage() {
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);
  const { data: onePassword } = useSuspenseQuery(onePasswordQuery);
  return (
    <Page>
      <PageHeader
        title="Settings"
        description="Updates, agents, 1Password, backup storage, and encryption for this shed instance."
      />
      <Stack gap={4}>
        <UpdatesCard />
        <AgentsCard />
        <OnePasswordCard status={onePassword} />
        <S3Card settings={settings} />
        <EncryptionCard settings={settings} />
      </Stack>
    </Page>
  );
}
