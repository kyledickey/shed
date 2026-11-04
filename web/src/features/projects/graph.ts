import type { Service, ServiceStatus, Variables } from "../../api/types";

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

/** Edge says service `from` uses service `to` through a variable reference. */
export type Edge = { from: string; to: string };

/** serviceEdges links each service to the services its variables reference, once per pair. */
export function serviceEdges(
  services: Pick<Service, "id" | "name">[],
  variables: (Variables | undefined)[],
): Edge[] {
  const ids = new Map(services.map((s) => [s.name, s.id]));
  const edges = new Map<string, Edge>();
  services.forEach((service, i) => {
    for (const value of Object.values(variables[i] ?? {})) {
      for (const name of parseRefs(value)) {
        const to = ids.get(name);
        if (to && to !== service.id) edges.set(`${service.id}>${to}`, { from: service.id, to });
      }
    }
  });
  return [...edges.values()];
}

/**
 * layers places each service at its longest dependency depth, so services
 * nobody uses sit first and the things they use follow. Each layer is
 * ordered by the average position of its users to cut crossings.
 */
export function layers(ids: string[], edges: Edge[]): string[][] {
  const depth = new Map(ids.map((id) => [id, 0]));
  // Relaxing every edge n times settles a DAG and stays bounded on cycles.
  for (let pass = 0; pass < ids.length; pass++) {
    let changed = false;
    for (const e of edges) {
      const d = (depth.get(e.from) ?? 0) + 1;
      if (d > (depth.get(e.to) ?? 0) && d < ids.length) {
        depth.set(e.to, d);
        changed = true;
      }
    }
    if (!changed) break;
  }
  const cols: string[][] = [];
  for (const id of ids) (cols[depth.get(id) ?? 0] ??= []).push(id);
  const placed = cols.filter(Boolean);
  const row = new Map<string, number>();
  placed.forEach((col, c) => {
    if (c > 0) {
      const weight = (id: string) => {
        const rows = edges.filter((e) => e.to === id).flatMap((e) => row.get(e.from) ?? []);
        return rows.length ? rows.reduce((a, b) => a + b, 0) / rows.length : Infinity;
      };
      col.sort((a, b) => weight(a) - weight(b));
    }
    col.forEach((id, i) => row.set(id, col.length > 1 ? i / (col.length - 1) : 0.5));
  });
  return placed;
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

/** Span is a stretch of a vertical stack, from position lo to hi, that needs a lane. */
export type Span = { key: string; lo: number; hi: number };

/**
 * lanes gives each span the lowest lane no earlier overlapping span holds,
 * so connectors running beside a stack of nodes never share a stretch of
 * line. Spans that only touch end to end share a lane.
 */
export function lanes(spans: Span[]): Map<string, number> {
  const ends: number[] = [];
  const lane = new Map<string, number>();
  for (const s of [...spans].sort((a, b) => a.lo - b.lo || a.hi - b.hi)) {
    let i = ends.findIndex((end) => end <= s.lo);
    if (i < 0) i = ends.length;
    ends[i] = s.hi;
    lane.set(s.key, i);
  }
  return lane;
}
