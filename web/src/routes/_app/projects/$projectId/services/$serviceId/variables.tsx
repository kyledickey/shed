import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { variablesQuery } from "../../../../../../api/services";
import { VariablesEditor } from "../../../../../../features/variables/VariablesEditor";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/variables")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(variablesQuery(params.serviceId)),
  component: VariablesPage,
});

function VariablesPage() {
  const { serviceId } = Route.useParams();
  const { data: variables } = useSuspenseQuery(variablesQuery(serviceId));
  return <VariablesEditor key={serviceId} serviceId={serviceId} variables={variables} />;
}
