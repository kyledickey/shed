import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { serviceQuery, variablesQuery } from "../../../../../../api/services";
import { Stack } from "../../../../../../components/Layout";
import { ProvidedVars } from "../../../../../../features/variables/ProvidedVars";
import { VariablesEditor } from "../../../../../../features/variables/VariablesEditor";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/variables")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(variablesQuery(params.serviceId)),
  component: VariablesPage,
});

function VariablesPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const { data: variables } = useSuspenseQuery(variablesQuery(serviceId));
  return (
    <Stack gap={4}>
      <VariablesEditor key={serviceId} serviceId={serviceId} variables={variables} />
      <ProvidedVars service={service} />
    </Stack>
  );
}
