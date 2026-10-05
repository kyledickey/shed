import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { History } from "lucide-react";
import { deploymentsQuery } from "../../../../../../api/deployments";
import { serviceQuery } from "../../../../../../api/services";
import { isPending, type Service } from "../../../../../../api/types";
import { Card } from "../../../../../../components/Card";
import { Stack } from "../../../../../../components/Layout";
import { EmptyState } from "../../../../../../components/Misc";
import { useDeploymentActions } from "../../../../../../features/deployments/actions";
import { DeploymentDetailDialog } from "../../../../../../features/deployments/DeploymentDetailDialog";
import { DeploymentHistory } from "../../../../../../features/deployments/DeploymentHistory";
import {
  InProgressCard,
  LatestProblem,
} from "../../../../../../features/deployments/LatestDeployment";

type DeploymentsSearch = { deployment?: string };

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/")({
  validateSearch: (search: Record<string, unknown>): DeploymentsSearch =>
    typeof search.deployment === "string" && search.deployment
      ? { deployment: search.deployment }
      : {},
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(deploymentsQuery(params.serviceId)),
  component: DeploymentsPage,
});

function DeploymentsPage() {
  const { serviceId } = Route.useParams();
  const { deployment: openId } = Route.useSearch();
  const navigate = Route.useNavigate();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const { data: deployments } = useSuspenseQuery(deploymentsQuery(serviceId));
  const actions = useDeploymentActions(service);

  const open = (id: string) => void navigate({ search: { deployment: id } });
  const close = () => void navigate({ search: {} });

  const latest = deployments[0];
  const serving = deployments.find((d) => d.status === "active");

  return (
    <>
      {latest ? (
        <Stack gap={4}>
          {isPending(latest.status) ? (
            <InProgressCard
              service={service}
              deployment={latest}
              onViewLogs={() => open(latest.id)}
            />
          ) : (
            <LatestProblem
              deployment={latest}
              serving={serving}
              onViewLogs={() => open(latest.id)}
            />
          )}
          <DeploymentHistory
            service={service}
            deployments={deployments}
            actions={actions}
            onOpen={open}
          />
        </Stack>
      ) : (
        <NoDeployments service={service} />
      )}
      <DeploymentDetailDialog
        service={service}
        deploymentId={openId}
        deployments={deployments}
        actions={actions}
        onClose={close}
      />
    </>
  );
}

function NoDeployments({ service }: { service: Service }) {
  const description = service.repo
    ? `Deploy to build ${service.repo} from ${service.branch}.` +
      (service.autoDeploy ? ` Pushes to ${service.branch} deploy automatically.` : "")
    : service.image
      ? `Deploy to pull ${service.image} and start it.`
      : "Deploy to start this service.";

  return (
    <Card>
      <EmptyState icon={<History />} title="No deployments yet" description={description} />
    </Card>
  );
}
