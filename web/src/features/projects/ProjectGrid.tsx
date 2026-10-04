import { Link } from "@tanstack/react-router";
import type { Project } from "../../api/types";
import { StatusDot } from "../../components/StatusDot";
import styles from "./ProjectGrid.module.css";

export function ProjectGrid({ projects }: { projects: Project[] }) {
  return (
    <div className={styles.grid}>
      {projects.map((project) => (
        <ProjectCard key={project.id} project={project} />
      ))}
    </div>
  );
}

const MAX_SERVICES = 4;

function ProjectCard({ project }: { project: Project }) {
  const { services } = project;
  const hidden = services.length - MAX_SERVICES;
  return (
    <Link to="/projects/$projectId" params={{ projectId: project.id }} className={styles.card}>
      <div className={styles.name}>{project.name}</div>
      <div className={styles.meta}>
        {services.length === 0
          ? "No services"
          : `${services.length} ${services.length === 1 ? "service" : "services"}`}
      </div>
      {services.length > 0 && (
        <ul className={styles.services}>
          {services.slice(0, MAX_SERVICES).map((s) => (
            <li key={s.id} className={styles.service}>
              <StatusDot status={s.status} label={false} />
              <span className={styles.serviceName}>{s.name}</span>
            </li>
          ))}
          {hidden > 0 && <li className={styles.more}>+{hidden} more</li>}
        </ul>
      )}
    </Link>
  );
}
