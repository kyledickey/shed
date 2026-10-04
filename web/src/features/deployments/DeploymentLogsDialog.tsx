import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { invalidateDeployments } from "../../api/deployments";
import { useLogStream } from "../../api/events";
import { isPending, type Deployment } from "../../api/types";
import { Dialog } from "../../components/Dialog";
import { LogViewer } from "../../components/LogViewer";
import { RelativeTime } from "../../components/RelativeTime";
import { StatusDot } from "../../components/StatusDot";
import { DeploymentSummary } from "./DeploymentSummary";
import styles from "./DeploymentLogsDialog.module.css";

type DeploymentLogsDialogProps = {
  serviceId: string;
  deployment: Deployment | null;
  onClose: () => void;
};

export function DeploymentLogsDialog({
  serviceId,
  deployment,
  onClose,
}: DeploymentLogsDialogProps) {
  // Keep showing the last deployment while the dialog animates closed.
  const [shown, setShown] = useState(deployment);
  if (deployment && deployment !== shown) setShown(deployment);

  return (
    <Dialog
      open={!!deployment}
      onOpenChange={(open) => !open && onClose()}
      title="Build logs"
      size="xl"
    >
      {shown && <BuildLogs key={shown.id} serviceId={serviceId} deployment={shown} />}
    </Dialog>
  );
}

function BuildLogs({ serviceId, deployment }: { serviceId: string; deployment: Deployment }) {
  const qc = useQueryClient();
  const { lines, state } = useLogStream(`/api/deployments/${deployment.id}/logs`, () => {
    void invalidateDeployments(qc, serviceId);
  });
  const following = isPending(deployment.status) && state === "open";

  return (
    <div className={styles.body}>
      <div className={styles.header}>
        <StatusDot status={deployment.status} />
        <DeploymentSummary deployment={deployment} />
        <span className={styles.time}>
          <RelativeTime iso={deployment.createdAt} />
        </span>
      </div>
      {deployment.error && <p className={styles.error}>{deployment.error}</p>}
      <LogViewer
        lines={lines}
        empty={state === "connecting" || following ? "Waiting for output…" : "No build output."}
      />
    </div>
  );
}
