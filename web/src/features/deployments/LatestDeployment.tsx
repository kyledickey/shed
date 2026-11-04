import { Button, LayerCard } from "@cloudflare/kumo";
import { ProhibitIcon, ScrollIcon, XCircleIcon } from "@phosphor-icons/react";
import { useEffect, useState, type ReactNode } from "react";
import type { Deployment, Service } from "../../api/types";
import { DeploySteps } from "../../components/deploy/Deploy";
import { Notice } from "../../components/Notice";
import { Pill } from "../../components/Pill";
import { RecordEmpty } from "../../components/RecordList";
import { imageName, shortSha } from "../../lib/names";
import { formatDate, formatDuration, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { CommitMeta, CommitSha, deploymentTitle, triggerLabel } from "./CommitMeta";
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

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className={styles.fact}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/**
 * ActiveDeployment is the deployment serving traffic, as a row of facts:
 * version, when it went live, where it came from, and how long it took.
 */
export function ActiveDeployment({
  service,
  deployment: d,
  onViewLogs,
}: {
  service: Service;
  deployment: Deployment | undefined;
  onViewLogs: (id: string) => void;
}) {
  return (
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.headTitle}>Active deployment</span>
        {d && service.status === "stopped" && <Pill>Stopped</Pill>}
        {d && (
          <Button
            size="sm"
            variant="ghost"
            icon={<ScrollIcon size={14} />}
            className={styles.headAction}
            onClick={() => onViewLogs(d.id)}
          >
            Build logs
          </Button>
        )}
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        {d ? (
          <dl className={styles.facts}>
            <Fact label={d.commitSha ? "Commit" : "Image"}>
              {d.commitSha ? (
                <CommitSha service={service} deployment={d} />
              ) : (
                <span className={styles.mono}>{d.image ? imageName(d.image) : "–"}</span>
              )}
            </Fact>
            <Fact label="Deployed">
              <time dateTime={d.finishedAt ?? d.createdAt} title={formatDate(d.createdAt)}>
                {relativeTime(d.finishedAt ?? d.createdAt)}
              </time>
            </Fact>
            <Fact label="Source">
              <Pill>{triggerLabel(d)}</Pill>
            </Fact>
            {service.repo && <Fact label="Branch">{service.branch}</Fact>}
            {d.commitAuthor && <Fact label="Author">{d.commitAuthor}</Fact>}
            {d.startedAt && (
              <Fact label="Build time">{formatDuration(d.startedAt, d.finishedAt)}</Fact>
            )}
            <div className={styles.messageFact}>
              <dt>Message</dt>
              <dd className={styles.message} title={deploymentTitle(d)}>
                {deploymentTitle(d)}
              </dd>
            </div>
          </dl>
        ) : (
          <RecordEmpty>No deployment is serving traffic.</RecordEmpty>
        )}
      </LayerCard.Primary>
    </LayerCard>
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
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.headTitle}>
          <HelpTip topic="pipeline">In progress</HelpTip>
        </span>
        <span className={styles.elapsed}>{formatDuration(since, null)}</span>
        <Button
          size="sm"
          icon={<ScrollIcon size={14} />}
          className={styles.headAction}
          onClick={onViewLogs}
          aria-label="View logs"
        >
          <span className={styles.hideNarrow}>View logs</span>
        </Button>
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.progress}>
        <div className={styles.commit}>
          <span className={styles.title}>{deploymentTitle(d)}</span>
          <CommitMeta service={service} deployment={d} />
        </div>
        <DeploySteps status={d.status} />
        {d.status === "waiting" && <WaitingForCi deployment={d} />}
      </LayerCard.Primary>
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
    <Notice
      icon={severe ? <XCircleIcon size={16} /> : <ProhibitIcon size={16} />}
      title={`${problem.title}: ${deploymentTitle(d)}`}
      danger={severe}
      actions={
        <Button size="sm" onClick={onViewLogs}>
          View logs
        </Button>
      }
    >
      {d.error || problem.fallback}
      {serving && serving.id !== d.id && (
        <span className={styles.serving}> The previous deployment is still serving traffic.</span>
      )}
    </Notice>
  );
}
