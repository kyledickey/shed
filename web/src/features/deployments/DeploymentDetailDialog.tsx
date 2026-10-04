import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, CircleX, RotateCcw, Square, Undo2 } from "lucide-react";
import { useState } from "react";
import { api, errorMessage } from "../../api/client";
import { invalidateDeployments } from "../../api/deployments";
import { useLogStream } from "../../api/events";
import { keys } from "../../api/keys";
import { isPending, type Deployment, type Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { DeploySteps } from "../../components/deploy/Deploy";
import { BuildLogView } from "../../components/logs/BuildLogView";
import { Callout } from "../../components/Misc";
import { Dialog } from "../../components/Overlay";
import { formatDate, formatDuration } from "../../lib/time";
import { redeployLabel, type DeploymentActions } from "./actions";
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

  // Keep showing the last deployment while the dialog animates closed.
  const [shown, setShown] = useState(deployment);
  if (deployment && deployment !== shown) setShown(deployment);
  const d = deploymentId ? deployment : shown;

  if (!d) {
    return (
      <Dialog
        open={!!deploymentId}
        onOpenChange={(open) => !open && onClose()}
        title="Deployment"
        size="lg"
      >
        {single.error ? (
          <Callout tone="tomato" icon={<CircleX size={16} />} title="Couldn't load deployment">
            {errorMessage(single.error)}
          </Callout>
        ) : (
          <p className={styles.quiet}>Loading…</p>
        )}
      </Dialog>
    );
  }

  const redeploy = redeployLabel(d);
  const pending = isPending(d.status);
  const failed = d.status === "failed" || d.status === "crashed";

  return (
    <Dialog
      open={!!deploymentId}
      onOpenChange={(open) => !open && onClose()}
      size="lg"
      title={
        <span className={styles.title}>
          <StatusBadge kind="deployment" status={d.status} size="sm" />
          <span className={styles.titleText}>{deploymentTitle(d)}</span>
        </span>
      }
      description={
        <CommitMeta service={service} deployment={d}>
          <span>{formatDate(d.createdAt)}</span>
          {d.startedAt && <span>{formatDuration(d.startedAt, d.finishedAt)}</span>}
        </CommitMeta>
      }
      footer={
        (pending || redeploy) && (
          <>
            {pending && (
              <Button
                variant="danger"
                onClick={() => actions.cancel(d)}
                loading={actions.isCanceling(d)}
              >
                {!actions.isCanceling(d) && <Square size={14} />}
                Cancel deployment
              </Button>
            )}
            {redeploy && (
              <Button onClick={() => actions.redeploy(d)} loading={actions.isRedeploying(d)}>
                {!actions.isRedeploying(d) &&
                  (d.status === "removed" ? <Undo2 size={14} /> : <RotateCcw size={14} />)}
                {redeploy}
              </Button>
            )}
          </>
        )
      }
    >
      {d.error && (
        <Callout
          tone={failed ? "tomato" : "neutral"}
          icon={failed ? <CircleX size={16} /> : <Ban size={16} />}
        >
          <span className={styles.error}>{d.error}</span>
        </Callout>
      )}
      {(pending || failed) && (
        <DeploySteps status={d.status} failedAt={d.image ? "deploying" : "building"} />
      )}
      {d.status === "waiting" && <WaitingForCi deployment={d} />}
      <BuildLogs key={d.id} serviceId={service.id} deployment={d} />
    </Dialog>
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
