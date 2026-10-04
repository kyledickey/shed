import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { History } from "lucide-react";
import { deploymentsQuery } from "../../../../../../api/deployments";
import { EmptyState } from "../../../../../../components/EmptyState";
import { DeploymentList } from "../../../../../../features/deployments/DeploymentList";
import { DeploymentLogsDialog } from "../../../../../../features/deployments/DeploymentLogsDialog";

type DeploymentsSearch = { logs?: string };

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/")({
  validateSearch: (search): DeploymentsSearch =>
    typeof search.logs === "string" ? { logs: search.logs } : {},
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(deploymentsQuery(params.serviceId)),
  component: DeploymentsPage,
});

function DeploymentsPage() {
  const { serviceId } = Route.useParams();
  const { logs } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { data: deployments } = useSuspenseQuery(deploymentsQuery(serviceId));
  const openLogs = (id?: string) => navigate({ search: id ? { logs: id } : {} });
  const selected = deployments.find((d) => d.id === logs);

  if (deployments.length === 0) {
    return (
      <EmptyState icon={<History size={18} />} title="No deployments yet">
        Click Deploy to build and start this service. Pushes to the configured branch deploy
        automatically.
      </EmptyState>
    );
  }

  return (
    <>
      <DeploymentList serviceId={serviceId} deployments={deployments} onViewLogs={openLogs} />
      <DeploymentLogsDialog
        serviceId={serviceId}
        deployment={selected ?? null}
        onClose={() => openLogs()}
      />
    </>
  );
}
