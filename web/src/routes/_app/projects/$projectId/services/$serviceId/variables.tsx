import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { onePasswordQuery } from "../../../../../../api/onepassword";
import { serviceQuery, variablesQuery } from "../../../../../../api/services";
import { Stack } from "../../../../../../components/Layout";
import { ProvidedVars } from "../../../../../../features/variables/ProvidedVars";
import {
  VariablesEditor,
  VariablesUnavailable,
} from "../../../../../../features/variables/VariablesEditor";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/variables")({
  // A failed read (a missing 1Password item, or 1Password down) is shown in
  // the card instead of failing the page.
  loader: ({ context: { queryClient }, params }) =>
    Promise.all([
      queryClient.ensureQueryData(onePasswordQuery),
      queryClient.prefetchQuery(variablesQuery(params.serviceId)),
    ]),
  component: VariablesPage,
});

function VariablesPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const variables = useQuery({ ...variablesQuery(serviceId), retryOnMount: false });
  return (
    <Stack gap={4}>
      {variables.data ? (
        <VariablesEditor key={serviceId} serviceId={serviceId} variables={variables.data} />
      ) : (
        variables.error && <VariablesUnavailable serviceId={serviceId} error={variables.error} />
      )}
      <ProvidedVars service={service} />
    </Stack>
  );
}
