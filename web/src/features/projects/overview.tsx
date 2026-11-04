import { useQueries, type UseQueryResult } from "@tanstack/react-query";
import { useMemo } from "react";
import { variablesQuery } from "../../api/services";
import type { Service, Variables } from "../../api/types";
import { serviceEdges, type Edge } from "./graph";

const allData = (results: UseQueryResult<Variables>[]) => results.map((r) => r.data);

/** useServiceEdges works out which services use which from their variable references. */
export function useServiceEdges(services: Service[]): Edge[] {
  const variables = useQueries({
    queries: services.map((s) => variablesQuery(s.id)),
    combine: allData,
  });
  return useMemo(() => serviceEdges(services, variables), [services, variables]);
}

/** privateAddress is host:port, or just the host when no port is set. */
export function privateAddress(service: Pick<Service, "port" | "privateHost">): string {
  return service.port > 0 ? `${service.privateHost}:${service.port}` : service.privateHost;
}
