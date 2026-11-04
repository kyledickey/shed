import { Button, Dialog } from "@cloudflare/kumo";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowCounterClockwiseIcon,
  ArrowSquareOutIcon,
  ArrowUUpLeftIcon,
  ProhibitIcon,
  XCircleIcon,
  XIcon,
} from "@phosphor-icons/react";
import { useState, type ReactNode } from "react";
import { api, errorMessage } from "../../api/client";
import { invalidateDeployments } from "../../api/deployments";
import { useLogStream } from "../../api/events";
import { keys } from "../../api/keys";
import { traceHref } from "../../api/logHistory";
import { deploymentOverviewUrl, serviceTelemetryQuery } from "../../api/telemetry";
import { isPending, type Deployment, type Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { DeploySteps } from "../../components/deploy/Deploy";
import { BuildLogView } from "../../components/logs/BuildLogView";
import { Notice } from "../../components/Notice";
import { formatDate, formatDuration } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { imageGone, imageGoneLabel, redeployLabel, type DeploymentActions } from "./actions";
import { CommitMeta, deploymentTitle } from "./CommitMeta";
import styles from "./DeploymentDetailDialog.module.css";
import { WaitingForCi } from "./LatestDeployment";

type DeploymentDetailDialogProps = {
  service: Service;
  /** The deployment to show; the dialog is closed when unset. */
  deploymentId: string | undefined;
  /** The cached history, used before fetching the deployment on its own. */
  deployments: Deployment[];
  actions: DeploymentActions;
  onClose: () => void;
};

/** DeploymentDetailDialog shows one deployment: status, error and streaming build log. */
export function DeploymentDetailDialog({
  service,
  deploymentId,
  deployments,
  actions,
  onClose,
}: DeploymentDetailDialogProps) {
  const listed = deployments.find((d) => d.id === deploymentId);
  // Deep links can point past the 50 most recent deployments.
  const single = useQuery({
    queryKey: [...keys.deployments(service.id), deploymentId],
    queryFn: () => api.get<Deployment>(`/deployments/${deploymentId}`),
    enabled: !!deploymentId && !listed,
  });
  const deployment = listed ?? (deploymentId ? single.data : undefined);
  const { data: telemetry } = useQuery({
    ...serviceTelemetryQuery(service.id),
    enabled: !!deploymentId,
  });

  // Keep showing the last deployment while the dialog animates closed.
  const [shown, setShown] = useState(deployment);
  if (deployment && deployment !== shown) setShown(deployment);
  const d = deploymentId ? deployment : shown;

  if (!d) {
    return (
      <Shell open={!!deploymentId} onClose={onClose} title="Deployment">
        {single.error ? (
          <Notice icon={<XCircleIcon size={16} />} title="Couldn't load deployment" danger>
            {errorMessage(single.error)}
          </Notice>
        ) : (
          <p className={styles.quiet}>Loading…</p>
        )}
      </Shell>
    );
  }

  const redeploy = redeployLabel(d);
  const pending = isPending(d.status);
  const failed = d.status === "failed" || d.status === "crashed";
  const overview = telemetry && deploymentOverviewUrl(telemetry, d);

  return (
    <Shell
      open={!!deploymentId}
      onClose={onClose}
      title={
        <>
          <StatusBadge kind="deployment" status={d.status} />
          <span className={styles.titleText}>
            <HelpTip topic="buildLog">{deploymentTitle(d)}</HelpTip>
          </span>
        </>
      }
      description={
        <CommitMeta service={service} deployment={d}>
          <span>{formatDate(d.createdAt)}</span>
          {d.startedAt && <span>{formatDuration(d.startedAt, d.finishedAt)}</span>}
          {overview && (
            <a className={styles.link} href={overview} target="_blank" rel="noreferrer">
              Open in Grafana <ArrowSquareOutIcon size={12} />
            </a>
          )}
          {d.traceId && (
            <a
              className={styles.link}
              href={traceHref(d.traceId)}
              target="_blank"
              rel="noreferrer"
              title="Open this deployment's trace in Grafana"
            >
              View trace <ArrowSquareOutIcon size={12} />
            </a>
          )}
        </CommitMeta>
      }
      footer={
        (pending || redeploy) && (
          <>
            {pending && (
              <Button
                variant="secondary-destructive"
                icon={<XIcon size={14} />}
                onClick={() => actions.cancel(d)}
                loading={actions.isCanceling(d)}
              >
                Cancel deployment
              </Button>
            )}
            {redeploy && (
              <Button
                icon={
                  d.status === "removed" ? (
                    <ArrowUUpLeftIcon size={14} />
                  ) : (
                    <ArrowCounterClockwiseIcon size={14} />
                  )
                }
                onClick={() => actions.redeploy(d)}
                loading={actions.isRedeploying(d)}
                disabled={imageGone(d)}
              >
                {imageGone(d) ? imageGoneLabel : redeploy}
              </Button>
            )}
          </>
        )
      }
    >
      {d.error && (
        <Notice
          icon={failed ? <XCircleIcon size={16} /> : <ProhibitIcon size={16} />}
          title={
            d.status === "crashed"
              ? "Container crashed"
              : failed
                ? "Deployment failed"
                : "Deployment stopped"
          }
          danger={failed}
        >
          {d.error}
        </Notice>
      )}
      {(pending || failed) && (
        <DeploySteps status={d.status} failedAt={d.image ? "deploying" : "building"} />
      )}
      {d.status === "waiting" && <WaitingForCi deployment={d} />}
      <BuildLogs key={d.id} serviceId={service.id} deployment={d} />
    </Shell>
  );
}

/** Shell is the dialog frame: a title row with a close button, the body, and a footer. */
function Shell({
  open,
  onClose,
  title,
  description,
  footer,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => !next && onClose()}>
      <Dialog size="xl" className={styles.dialog}>
        <header className={styles.header}>
          <div className={styles.heading}>
            <Dialog.Title className={styles.title}>{title}</Dialog.Title>
            {description && (
              <Dialog.Description render={<div />} className={styles.description}>
                {description}
              </Dialog.Description>
            )}
          </div>
          <Dialog.Close
            render={
              <Button
                variant="ghost"
                size="sm"
                shape="square"
                icon={<XIcon size={16} />}
                aria-label="Close"
              />
            }
            aria-label="Close"
          />
        </header>
        <div className={styles.body}>{children}</div>
        {footer && <footer className={styles.footer}>{footer}</footer>}
      </Dialog>
    </Dialog.Root>
  );
}
function BuildLogs({ serviceId, deployment }: { serviceId: string; deployment: Deployment }) {
  const qc = useQueryClient();
  const { lines, state } = useLogStream(`/api/deployments/${deployment.id}/logs`, () => {
    void invalidateDeployments(qc, serviceId);
  });
  if (lines.length === 0 && state === "ended" && !isPending(deployment.status)) {
    return <p className={styles.quiet}>No build output was recorded for this deployment.</p>;
  }
  return <BuildLogView lines={lines} state={state} height={420} />;
}
