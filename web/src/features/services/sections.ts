import type { Service } from "../../api/types";

export type ServiceTab = "deployments" | "logs" | "metrics" | "variables" | "backups" | "settings";

export const serviceTabs: { value: ServiceTab; label: string }[] = [
  { value: "deployments", label: "Deployments" },
  { value: "logs", label: "Logs" },
  { value: "metrics", label: "Metrics" },
  { value: "variables", label: "Variables" },
  { value: "backups", label: "Backups" },
  { value: "settings", label: "Settings" },
];

/** visibleServiceTabs hides Backups from services that have no volume to back up. */
export function visibleServiceTabs(service: Pick<Service, "volumes">) {
  return serviceTabs.filter((t) => t.value !== "backups" || service.volumes.length > 0);
}

export function serviceTabHref(service: Pick<Service, "id" | "projectId">, tab: ServiceTab) {
  const base = `/projects/${service.projectId}/services/${service.id}`;
  return tab === "deployments" ? base : `${base}/${tab}`;
}

/** serviceTabOf reads the active section from a pathname under the service. */
export function serviceTabOf(pathname: string): ServiceTab {
  const last = pathname.replace(/\/$/, "").split("/").pop();
  return serviceTabs.find((t) => t.value === last)?.value ?? "deployments";
}
