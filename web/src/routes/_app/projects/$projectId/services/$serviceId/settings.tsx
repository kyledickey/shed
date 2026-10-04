import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../../api/services";
import { BuildCard, DeployCard, SourceCard } from "../../../../../../features/settings/AppCards";
import { DangerCard } from "../../../../../../features/settings/DangerCard";
import { NetworkingCard } from "../../../../../../features/settings/NetworkingCard";
import { VolumesCard } from "../../../../../../features/settings/VolumesCard";
import { Stack } from "../../../../../../components/Layout";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/settings")({
  component: ServiceSettingsPage,
});

function ServiceSettingsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const isApp = service.kind === "app";
  return (
    <Stack key={service.id} gap={4}>
      <SourceCard service={service} />
      {isApp && <BuildCard service={service} />}
      {isApp && <DeployCard service={service} />}
      <NetworkingCard service={service} />
      <VolumesCard service={service} />
      <DangerCard service={service} />
    </Stack>
  );
}
