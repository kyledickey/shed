import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet, useLocation } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../api/services";
import { Page } from "../../../../../components/Layout";
import { RedeployHintProvider } from "../../../../../features/services/redeploy";
import { ServiceHeader } from "../../../../../features/services/ServiceHeader";
import { serviceTabOf } from "../../../../../features/services/sections";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(serviceQuery(params.serviceId)),
  component: ServiceLayout,
});

function ServiceLayout() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const pathname = useLocation({ select: (l) => l.pathname });
  return (
    <RedeployHintProvider key={service.id}>
      <ServiceHeader service={service} tab={serviceTabOf(pathname)} />
      <Page>
        <Outlet />
      </Page>
    </RedeployHintProvider>
  );
}
