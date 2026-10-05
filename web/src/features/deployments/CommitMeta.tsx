import { CloudUpload, GitCommitHorizontal, Rocket, RotateCcw } from "lucide-react";
import type { ReactNode } from "react";
import type { Deployment, DeploymentTrigger, Service } from "../../api/types";
import { Avatar } from "../../components/Misc";
import { shortSha } from "../../lib/names";
import styles from "./CommitMeta.module.css";

const triggers: Record<DeploymentTrigger, { label: string; icon: ReactNode }> = {
  push: { label: "Push", icon: <GitCommitHorizontal size={12} /> },
  manual: { label: "Manual", icon: <Rocket size={12} /> },
  redeploy: { label: "Redeploy", icon: <RotateCcw size={12} /> },
  create: { label: "Initial deploy", icon: <CloudUpload size={12} /> },
};

/** deploymentTitle is a one-line name: the commit subject, else the image. */
export function deploymentTitle(d: Deployment): string {
  return d.commitMessage.split("\n")[0]?.trim() || d.image || "Deployment";
}

/** commitUrl links a deployment's commit on GitHub, for repo apps. */
export function commitUrl(service: Pick<Service, "repo">, d: Deployment): string | null {
  return service.repo && d.commitSha
    ? `https://github.com/${service.repo}/commit/${d.commitSha}`
    : null;
}

/** CommitMeta is the quiet line under a deployment title: sha, trigger, author, extras. */
export function CommitMeta({
  service,
  deployment: d,
  children,
}: {
  service: Pick<Service, "repo">;
  deployment: Deployment;
  children?: ReactNode;
}) {
  const url = commitUrl(service, d);
  const trigger = triggers[d.trigger];
  return (
    <span className={styles.meta}>
      {d.commitSha &&
        (url ? (
          <a className={styles.sha} href={url} target="_blank" rel="noreferrer">
            <GitCommitHorizontal size={12} />
            {shortSha(d.commitSha)}
          </a>
        ) : (
          <span className={styles.sha}>
            <GitCommitHorizontal size={12} />
            {shortSha(d.commitSha)}
          </span>
        ))}
      <span className={styles.item}>
        {trigger.icon}
        {trigger.label}
      </span>
      {d.commitAuthor && (
        <span className={styles.item}>
          <Avatar name={d.commitAuthor} size={16} />
          {d.commitAuthor}
        </span>
      )}
      {children}
    </span>
  );
}
