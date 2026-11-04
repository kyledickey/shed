import type { ReactNode } from "react";
import type { Deployment, DeploymentTrigger, Service } from "../../api/types";
import { Pill } from "../../components/Pill";
import { shortSha } from "../../lib/names";
import styles from "./CommitMeta.module.css";

const triggers: Record<DeploymentTrigger, string> = {
  push: "Push",
  manual: "Manual",
  redeploy: "Redeploy",
  create: "Initial deploy",
  telemetry: "Telemetry",
};

/** triggerLabel names what started a deployment, for its source pill. */
export function triggerLabel(d: Deployment): string {
  return triggers[d.trigger];
}

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

/** CommitSha is a deployment's short SHA, linked to GitHub when it can be. */
export function CommitSha({
  service,
  deployment: d,
}: {
  service: Pick<Service, "repo">;
  deployment: Deployment;
}) {
  if (!d.commitSha) return null;
  const url = commitUrl(service, d);
  return url ? (
    <a className={styles.sha} href={url} target="_blank" rel="noreferrer">
      {shortSha(d.commitSha)}
    </a>
  ) : (
    <span className={styles.sha}>{shortSha(d.commitSha)}</span>
  );
}

/** CommitMeta is the quiet line under a deployment title: sha, source, author, extras. */
export function CommitMeta({
  service,
  deployment: d,
  children,
}: {
  service: Pick<Service, "repo">;
  deployment: Deployment;
  children?: ReactNode;
}) {
  return (
    <span className={styles.meta}>
      <CommitSha service={service} deployment={d} />
      <Pill>{triggerLabel(d)}</Pill>
      {d.commitAuthor && <span>by {d.commitAuthor}</span>}
      {children}
    </span>
  );
}
