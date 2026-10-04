import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../../api/services";
import { ServiceSettings } from "../../../../../../features/settings/ServiceSettings";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/settings")({
  component: SettingsPage,
});

function SettingsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  return <ServiceSettings key={service.id} service={service} />;
}
