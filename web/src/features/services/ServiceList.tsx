import { useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useEffect } from "react";
import { keys } from "../../api/keys";
import type { ProjectDetail, Service } from "../../api/types";
import { RelativeTime } from "../../components/RelativeTime";
import { kindLabels, ServiceIcon } from "../../components/ServiceIcon";
import { StatusDot } from "../../components/StatusDot";
import styles from "./ServiceList.module.css";

export function ServiceList({ project }: { project: ProjectDetail }) {
  const qc = useQueryClient();

  useEffect(() => {
    for (const service of project.services) qc.setQueryData(keys.service(service.id), service);
  }, [project, qc]);

  return (
    <ul className={styles.list}>
      {project.services.map((service) => (
        <li key={service.id}>
          <Link
            to="/projects/$projectId/services/$serviceId"
            params={{ projectId: project.id, serviceId: service.id }}
            className={styles.row}
          >
            <ServiceIcon kind={service.kind} repo={service.repo} />
            <div className={styles.main}>
              <div className={styles.name}>{service.name}</div>
              <div className={styles.source}>{serviceSource(service)}</div>
            </div>
            <div className={styles.time}>
              {service.latestDeployment && (
                <RelativeTime iso={service.latestDeployment.createdAt} />
              )}
            </div>
            <div className={styles.status}>
              <StatusDot status={service.status} />
            </div>
          </Link>
        </li>
      ))}
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
