import { Empty, InputGroup, LayerCard } from "@cloudflare/kumo";
import { MagnifyingGlassIcon, StackIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import type { Project } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { ServiceIcon } from "../../components/Misc";
import { relativeTime } from "../../lib/time";
import { projectStatus } from "./graph";
import styles from "./ProjectList.module.css";

/**
 * ProjectList is the home page's list: a search box over one row per
 * project with its services, its rolled-up status, and its age.
 */
export function ProjectList({ projects }: { projects: Project[] }) {
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();
  const matches = projects.filter(
    (p) =>
      p.name.toLowerCase().includes(query) ||
      p.services.some((s) => s.name.toLowerCase().includes(query)),
  );

  return (
    <div className={styles.stack}>
      <div className={styles.toolbar}>
        <InputGroup className={styles.search}>
          <InputGroup.Addon>
            <MagnifyingGlassIcon size={16} />
          </InputGroup.Addon>
          <InputGroup.Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Search projects and services"
            aria-label="Search projects and services"
            autoComplete="off"
          />
        </InputGroup>
      </div>
      <LayerCard className={styles.card}>
        <LayerCard.Secondary className={styles.head}>
          <span>Name</span>
          <span className={styles.servicesCol}>Services</span>
          <span>Status</span>
          <span className={styles.whenCol}>Created</span>
        </LayerCard.Secondary>
        <LayerCard.Primary className={styles.list}>
          {matches.map((p) => (
            <ProjectRow key={p.id} project={p} />
          ))}
          {matches.length === 0 && (
            <p className={styles.noMatch}>No projects or services match “{filter.trim()}”.</p>
          )}
        </LayerCard.Primary>
      </LayerCard>
    </div>
  );
}

function ProjectRow({ project }: { project: Project }) {
  const { services } = project;
  return (
    <Link to="/projects/$projectId" params={{ projectId: project.id }} className={styles.row}>
      <span className={styles.name}>{project.name}</span>
      <span className={styles.services}>
        {services.length === 0 && <span className={styles.none}>No services</span>}
        {services.map((s) => (
          <span key={s.id} className={styles.service}>
            <ServiceIcon kind={s.kind} size={24} />
            {s.name}
          </span>
        ))}
      </span>
      <span className={styles.status}>
        {services.length > 0 ? (
          <StatusBadge kind="service" status={projectStatus(services)} size="sm" />
        ) : (
          <span className={styles.none}>Empty</span>
        )}
      </span>
      <span className={styles.when}>{relativeTime(project.createdAt)}</span>
    </Link>
  );
}

/** NoProjects is the home page before the first project: a card that says so. */
export function NoProjects() {
  return (
    <Empty
      className={styles.empty}
      icon={<StackIcon size={48} className={styles.emptyIcon} />}
      title="No projects yet"
      description="A project holds the services of one app: a GitHub repo or a Docker image, and the databases it uses. Create one to deploy your first service."
    />
  );
}
