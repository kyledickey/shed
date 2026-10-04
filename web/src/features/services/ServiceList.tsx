import { useQueries } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { serviceQuery } from "../../api/services";
import type { Project, Service } from "../../api/types";
import { RelativeTime } from "../../components/RelativeTime";
import { kindLabels, ServiceIcon } from "../../components/ServiceIcon";
import { StatusDot } from "../../components/StatusDot";
import styles from "./ServiceList.module.css";

export function ServiceList({ project }: { project: Project }) {
  const details = useQueries({ queries: project.services.map((s) => serviceQuery(s.id)) });

  return (
    <ul className={styles.list}>
      {project.services.map((summary, i) => {
        const service = details[i]?.data;
        return (
          <li key={summary.id}>
            <Link
              to="/projects/$projectId/services/$serviceId"
              params={{ projectId: project.id, serviceId: summary.id }}
              className={styles.row}
            >
              <ServiceIcon kind={summary.kind} repo={service?.repo} />
              <div className={styles.main}>
                <div className={styles.name}>{summary.name}</div>
                <div className={styles.source}>
                  {service ? serviceSource(service) : kindLabels[summary.kind]}
                </div>
              </div>
              <div className={styles.time}>
                {service?.latestDeployment && (
                  <RelativeTime iso={service.latestDeployment.createdAt} />
                )}
              </div>
              <div className={styles.status}>
                <StatusDot status={service?.status ?? summary.status} />
              </div>
            </Link>
          </li>
        );
      })}
    </ul>
  );
}

/** The most useful one-line description of where a service lives or comes from. */
function serviceSource(service: Service): string {
  const domain = service.domains[0];
  if (domain) return domain.host;
  if (service.repo) return `${service.repo}@${service.branch}`;
  if (service.kind === "app") return service.image;
  return kindLabels[service.kind];
}
