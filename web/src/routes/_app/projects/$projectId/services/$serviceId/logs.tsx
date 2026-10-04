import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../../api/services";
import { RuntimeLogs } from "../../../../../../features/logs/RuntimeLogs";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/logs")({
  component: LogsPage,
});

function LogsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  return <RuntimeLogs key={service.id} service={service} />;
}
