import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../api/services";
import { Page } from "../../../../../components/Layout";
import { RedeployHintProvider } from "../../../../../features/services/redeploy";
import { ServiceHeader } from "../../../../../features/services/ServiceHeader";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(serviceQuery(params.serviceId)),
  component: ServiceLayout,
});

function ServiceLayout() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  return (
    <RedeployHintProvider key={service.id}>
      <Page>
        <ServiceHeader service={service} />
        <Outlet />
      </Page>
    </RedeployHintProvider>
  );
}
