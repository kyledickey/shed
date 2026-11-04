import { LayerCard } from "@cloudflare/kumo";
import { useQueries, type UseQueryResult } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { deploymentsQuery } from "../../api/deployments";
import type { Deployment, Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { shortSha } from "../../lib/names";
import { relativeTime } from "../../lib/time";
import styles from "./ProjectActivity.module.css";

const RECENT = 6;

const triggerLabels: Record<Deployment["trigger"], string> = {
  push: "Push",
  manual: "Manual",
  redeploy: "Redeploy",
  create: "Created",
  telemetry: "Telemetry",
};

/** RecentDeployments lists the project's newest deployments across all its apps. */
export function RecentDeployments({ services }: { services: Service[] }) {
  const apps = services.filter((s) => s.kind === "app");
  const lists = useQueries({
    queries: apps.map((s) => deploymentsQuery(s.id)),
    combine: (results: UseQueryResult<Deployment[]>[]) => results.map((r) => r.data ?? []),
  });
  const byService = new Map(apps.map((s) => [s.id, s]));
  const recent = lists
    .flat()
    .sort((a, b) => b.createdAt.localeCompare(a.createdAt))
    .slice(0, RECENT);

  return (
    <LayerCard>
      <LayerCard.Secondary>Recent deployments</LayerCard.Secondary>
      <LayerCard.Primary className={styles.list}>
        {recent.length === 0 && <p className={styles.empty}>No deployments yet.</p>}
        {recent.map((d) => {
          const service = byService.get(d.serviceId)!;
          return (
            <Link
              key={d.id}
              to="/projects/$projectId/services/$serviceId"
              params={{ projectId: service.projectId, serviceId: service.id }}
              className={styles.row}
            >
              <span className={styles.service}>{service.name}</span>
              <span className={styles.sha}>{d.commitSha ? shortSha(d.commitSha) : "image"}</span>
              <span className={styles.message}>
                {d.commitMessage.split("\n")[0] || "No commit message"}
              </span>
              <span className={styles.status}>
                <StatusBadge kind="deployment" status={d.status} size="sm" />
              </span>
              <span className={styles.trigger}>{triggerLabels[d.trigger]}</span>
              <span className={styles.when}>{relativeTime(d.createdAt)}</span>
            </Link>
          );
        })}
      </LayerCard.Primary>
    </LayerCard>
  );
}
