import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet } from "@tanstack/react-router";
import { serviceQuery } from "../../../../../api/services";
import { Page } from "../../../../../components/Page";
import { TabLink, Tabs } from "../../../../../components/Tabs";
import { RedeployHintProvider } from "../../../../../features/services/redeploy";
import { ServiceHeader } from "../../../../../features/services/ServiceHeader";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(serviceQuery(params.serviceId)),
  component: ServiceLayout,
});

function ServiceLayout() {
  const params = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(params.serviceId));
  const tab = { from: Route.fullPath, params } as const;

  return (
    <RedeployHintProvider key={service.id}>
      <Page>
        <ServiceHeader service={service} />
        <Tabs label="Service sections">
          <TabLink {...tab} to="." activeOptions={{ exact: true }}>
            Deployments
          </TabLink>
          <TabLink {...tab} to="./logs">
            Logs
          </TabLink>
          <TabLink {...tab} to="./variables">
            Variables
          </TabLink>
          <TabLink {...tab} to="./settings">
            Settings
          </TabLink>
        </Tabs>
        <Outlet />
      </Page>
    </RedeployHintProvider>
  );
}
