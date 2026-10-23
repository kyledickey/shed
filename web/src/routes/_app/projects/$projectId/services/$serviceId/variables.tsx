import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { onePasswordQuery } from "../../../../../../api/onepassword";
import { serviceQuery, variablesQuery } from "../../../../../../api/services";
import { Stack } from "../../../../../../components/Layout";
import { ProvidedVars } from "../../../../../../features/variables/ProvidedVars";
import { VariablesEditor } from "../../../../../../features/variables/VariablesEditor";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/variables")({
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(variablesQuery(params.serviceId)),
      context.queryClient.ensureQueryData(onePasswordQuery),
    ]),
  component: VariablesPage,
});

function VariablesPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const { data: variables } = useSuspenseQuery(variablesQuery(serviceId));
  const { data: onePassword } = useSuspenseQuery(onePasswordQuery);
  return (
    <Stack gap={4}>
      <VariablesEditor
        key={serviceId}
        service={service}
        variables={variables}
        connected={onePassword.connected}
      />
      <ProvidedVars service={service} />
    </Stack>
  );
}
