import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { backupSettingsQuery, serviceBackupsQuery } from "../../../../../../api/backups";
import { serviceQuery } from "../../../../../../api/services";
import { ServiceBackups } from "../../../../../../features/backups/ServiceBackups";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/backups")({
  loader: async ({ context: { queryClient }, params }) => {
    const service = await queryClient.ensureQueryData(serviceQuery(params.serviceId));
    // Services without volumes show an empty state and have no backups to load.
    if (service.volumes.length === 0) return;
    await Promise.all([
      queryClient.ensureQueryData(serviceBackupsQuery(params.serviceId)),
      queryClient.ensureQueryData(backupSettingsQuery),
    ]);
  },
  component: BackupsPage,
});

function BackupsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  return <ServiceBackups key={service.id} service={service} />;
}
