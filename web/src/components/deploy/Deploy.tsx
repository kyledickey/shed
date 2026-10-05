import {
  Check,
  CloudUpload,
  GitBranch,
  GitCommitHorizontal,
  Hammer,
  Hourglass,
  Rocket,
  RotateCcw,
  Undo2,
  X,
} from "lucide-react";
import type { ReactNode } from "react";
import type { Deployment, DeploymentStatus, DeploymentTrigger } from "../../api/types";
import { shortSha } from "../../lib/names";
import { formatDuration, relativeTime } from "../../lib/time";
import { StatusBadge } from "../Badge";
import { Avatar } from "../Misc";
import { deploymentLook } from "../tone";
import styles from "./deploy.module.css";

const triggerLabels: Record<DeploymentTrigger, { label: string; icon: ReactNode }> = {
  push: { label: "Push", icon: <GitCommitHorizontal size={12} /> },
  manual: { label: "Manual", icon: <Rocket size={12} /> },
  redeploy: { label: "Redeploy", icon: <RotateCcw size={12} /> },
  create: { label: "Created", icon: <CloudUpload size={12} /> },
};

type DeployRowProps = {
  deployment: Deployment;
  branch?: string;
  /** Marks the deployment serving traffic. */
  current?: boolean;
  actions?: ReactNode;
  onClick?: () => void;
};

/** DeployRow is one deployment in a history list. */
export function DeployRow({ deployment: d, branch, current, actions, onClick }: DeployRowProps) {
  const look = deploymentLook(d.status);
  const trigger = triggerLabels[d.trigger];
  return (
    <div className={styles.row} data-current={current || undefined}>
      <button type="button" className={styles.rowMain} onClick={onClick}>
        <span className={styles.rail} data-tone={look.tone} data-live={look.live || undefined}>
          <StatusGlyph status={d.status} />
        </span>
        <span className={styles.commit}>
          <span className={styles.message}>{d.commitMessage || "No commit message"}</span>
          <span className={styles.meta}>
            {d.commitSha && (
              <span className={styles.sha}>
                <GitCommitHorizontal size={12} />
                {shortSha(d.commitSha)}
              </span>
            )}
            {branch && (
              <span className={styles.metaItem}>
                <GitBranch size={12} />
                {branch}
              </span>
            )}
            <span className={styles.metaItem}>
              {trigger.icon}
              {trigger.label}
            </span>
            {d.commitAuthor && (
              <span className={styles.metaItem}>
                <Avatar name={d.commitAuthor} size={16} />
                {d.commitAuthor}
              </span>
            )}
          </span>
        </span>
        <span className={styles.side}>
          {current ? (
            <span className={styles.currentTag}>
              <span className={styles.currentDot} />
              Serving
            </span>
          ) : (
            <StatusBadge kind="deployment" status={d.status} size="sm" />
          )}
          <span className={styles.when}>
            {relativeTime(d.createdAt)}
            {d.startedAt && <> · {formatDuration(d.startedAt, d.finishedAt)}</>}
          </span>
        </span>
      </button>
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  );
}

function StatusGlyph({ status }: { status: DeploymentStatus }) {
  switch (status) {
    case "active":
      return <Check size={13} strokeWidth={2.5} />;
    case "failed":
    case "crashed":
      return <X size={13} strokeWidth={2.5} />;
    case "building":
      return <Hammer size={12} />;
    case "deploying":
      return <Rocket size={12} />;
    case "queued":
    case "waiting":
      return <Hourglass size={12} />;
    default:
      return <Undo2 size={12} />;
  }
}

const stages = [
  { key: "queued", label: "Queued" },
  { key: "building", label: "Build" },
  { key: "deploying", label: "Deploy" },
  { key: "active", label: "Live" },
] as const;

function stageIndex(status: DeploymentStatus): number {
  switch (status) {
    case "queued":
    case "waiting":
      return 0;
    case "building":
      return 1;
    case "deploying":
      return 2;
    default:
      return 3;
  }
}

/**
 * DeploySteps is a progress track through queued → build → deploy → live.
 * `failedAt` marks the stage that failed for failed deployments.
 */
export function DeploySteps({
  status,
  failedAt,
}: {
  status: DeploymentStatus;
  failedAt?: "building" | "deploying";
}) {
  const failed = status === "failed" || status === "crashed";
  const at = failed ? (failedAt === "deploying" ? 2 : 1) : stageIndex(status);
  return (
    <ol className={styles.steps}>
      {stages.map((s, i) => {
        const state =
          failed && i === at
            ? "failed"
            : i < at || status === "active"
              ? "done"
              : i === at
                ? "current"
                : "todo";
        return (
          <li key={s.key} className={styles.stage} data-state={state}>
            <span className={styles.node}>
              {state === "done" && <Check size={12} strokeWidth={3} />}
              {state === "failed" && <X size={12} strokeWidth={3} />}
              {state === "current" && <span className={styles.nodePulse} />}
            </span>
            <span className={styles.stageLabel}>{s.label}</span>
          </li>
        );
      })}
    </ol>
  );
}
