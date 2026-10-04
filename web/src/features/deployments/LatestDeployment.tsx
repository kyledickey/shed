import { Ban, CircleX, ScrollText } from "lucide-react";
import { useEffect, useState } from "react";
import type { Deployment, Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { DeploySteps } from "../../components/deploy/Deploy";
import { Callout } from "../../components/Misc";
import { shortSha } from "../../lib/names";
import { formatDuration } from "../../lib/time";
import { CommitMeta, deploymentTitle } from "./CommitMeta";
import styles from "./LatestDeployment.module.css";

/** useTick re-renders every second, for live elapsed times. */
function useTick() {
  const [, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(id);
  }, []);
}

/** WaitingForCi explains a deployment held back by the service's Wait for CI setting. */
export function WaitingForCi({ deployment: d }: { deployment: Deployment }) {
  return (
    <p className={styles.note}>
      Waiting for GitHub checks on{" "}
      {d.commitSha ? <code>{shortSha(d.commitSha)}</code> : "this commit"} to finish. shed builds
      once they pass and skips the commit if any fail. Turn off Wait for CI in settings to deploy
      without waiting.
    </p>
  );
}

/** InProgressCard tracks the deployment that is currently queued, building or starting. */
export function InProgressCard({
  service,
  deployment: d,
  onViewLogs,
}: {
  service: Service;
  deployment: Deployment;
  onViewLogs: () => void;
}) {
  useTick();
  const since = d.startedAt ?? d.createdAt;
  return (
    <LayerCard
      title="In progress"
      meta={<StatusBadge kind="deployment" status={d.status} size="sm" />}
      actions={
        <Button size="sm" onClick={onViewLogs} aria-label="View logs">
          <ScrollText size={13} />
          <span className={styles.hideNarrow}>View logs</span>
        </Button>
      }
      padded
    >
      <div className={styles.body}>
        <div className={styles.head}>
          <div className={styles.commit}>
            <span className={styles.title}>{deploymentTitle(d)}</span>
            <CommitMeta service={service} deployment={d} />
          </div>
          <span className={styles.elapsed}>{formatDuration(since, null)}</span>
        </div>
        <DeploySteps status={d.status} />
        {d.status === "waiting" && <WaitingForCi deployment={d} />}
      </div>
    </LayerCard>
  );
}

const problems: Partial<Record<Deployment["status"], { title: string; fallback: string }>> = {
  failed: { title: "Deployment failed", fallback: "The deployment failed." },
  crashed: { title: "Container crashed", fallback: "The container stopped unexpectedly." },
  skipped: { title: "Deployment skipped", fallback: "CI checks failed for this commit." },
  canceled: { title: "Deployment canceled", fallback: "The deployment was canceled." },
};

/**
 * LatestProblem explains why the newest deployment didn't go live, or
 * renders nothing when it did.
 */
export function LatestProblem({
  deployment: d,
  serving,
  onViewLogs,
}: {
  deployment: Deployment;
  /** The deployment still serving traffic, if any. */
  serving?: Deployment;
  onViewLogs: () => void;
}) {
  const problem = problems[d.status];
  if (!problem) return null;
  const severe = d.status === "failed" || d.status === "crashed";
  return (
    <Callout
      tone={severe ? "tomato" : "neutral"}
      icon={severe ? <CircleX size={16} /> : <Ban size={16} />}
      title={`${problem.title}: ${deploymentTitle(d)}`}
      actions={
        <Button size="sm" onClick={onViewLogs}>
          View logs
        </Button>
      }
    >
      <span className={styles.error}>{d.error || problem.fallback}</span>
      {serving && serving.id !== d.id && (
        <span className={styles.serving}> The previous deployment is still serving traffic.</span>
      )}
    </Callout>
  );
}
