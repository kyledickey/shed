import type { Service, ServiceStatus, Variables } from "../../api/types";
import type { MapEdge } from "../../components/ServiceMap";

const refPattern = /\$\{\{\s*([^\s{}]+)\s*\}\}/g;

/** parseRefs returns the service names referenced as ${{ service.KEY }} in value. */
export function parseRefs(value: string): string[] {
  const names: string[] = [];
  for (const m of value.matchAll(refPattern)) {
    const body = m[1] ?? "";
    const dot = body.indexOf(".");
    if (dot > 0) names.push(body.slice(0, dot));
  }
  return names;
}

/**
 * serviceEdges links each service to the services its variables reference.
 * One edge per pair; the label lists the referencing keys.
 */
export function serviceEdges(
  services: Pick<Service, "id" | "name">[],
  variables: (Variables | undefined)[],
): MapEdge[] {
  const ids = new Map(services.map((s) => [s.name, s.id]));
  const edges = new Map<string, MapEdge & { keys: string[] }>();
  services.forEach((service, i) => {
    for (const [key, value] of Object.entries(variables[i] ?? {})) {
      for (const name of parseRefs(value)) {
        const to = ids.get(name);
        if (!to || to === service.id) continue;
        const id = `${service.id}>${to}`;
        const edge = edges.get(id) ?? { from: service.id, to, keys: [] };
        if (!edge.keys.includes(key)) edge.keys.push(key);
        edges.set(id, edge);
      }
    }
  });
  return [...edges.values()].map(({ from, to, keys }) => ({
    from,
    to,
    label: keys.sort().join(", "),
  }));
}

/** projectStatus rolls service statuses up into one: busy, then broken, then up. */
export function projectStatus(services: { status: ServiceStatus }[]): ServiceStatus {
  const statuses = services.map((s) => s.status);
  if (statuses.includes("deploying")) return "deploying";
  if (statuses.includes("crashed")) return "crashed";
  if (statuses.includes("failed")) return "failed";
  if (statuses.includes("active")) return "active";
  if (statuses.includes("stopped")) return "stopped";
  return "offline";
}

/** servicesLabel is "1 service" / "N services". */
export function servicesLabel(n: number): string {
  return n === 0 ? "No services" : `${n} ${n === 1 ? "service" : "services"}`;
}
