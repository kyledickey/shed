import { Link } from "@tanstack/react-router";
import { Layers, Plus } from "lucide-react";
import type { Project } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { Card } from "../../components/Card";
import { Grid } from "../../components/Layout";
import { ServiceIcon } from "../../components/Misc";
import { projectStatus, servicesLabel } from "./graph";
import styles from "./ProjectGrid.module.css";

const MAX_ICONS = 5;

/** ProjectGrid lists project cards followed by a "New project" tile. */
export function ProjectGrid({ projects, onNew }: { projects: Project[]; onNew: () => void }) {
  return (
    <Grid min={260}>
      {projects.map((p) => (
        <ProjectCard key={p.id} project={p} />
      ))}
      <button type="button" className={styles.newTile} onClick={onNew}>
        <Plus size={20} />
        New project
      </button>
    </Grid>
  );
}

function ProjectCard({ project }: { project: Project }) {
  const { services } = project;
  const extra = services.length - MAX_ICONS;
  return (
    <Card interactive className={styles.card}>
      <div className={styles.top}>
        <div className={styles.icons}>
          {services.slice(0, MAX_ICONS).map((s) => (
            <ServiceIcon key={s.id} kind={s.kind} size={30} />
          ))}
          {extra > 0 && <span className={styles.more}>+{extra}</span>}
          {services.length === 0 && <ServiceIcon icon={<Layers />} size={30} />}
        </div>
        {services.length > 0 && (
          <StatusBadge kind="service" status={projectStatus(services)} size="sm" />
        )}
      </div>
      <div className={styles.text}>
        <Link to="/projects/$projectId" params={{ projectId: project.id }} className={styles.name}>
          {project.name}
        </Link>
        <div className={styles.meta}>{servicesLabel(services.length)}</div>
      </div>
    </Card>
  );
}
