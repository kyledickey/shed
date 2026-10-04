import { Ban, Ellipsis, FileText, RotateCcw } from "lucide-react";
import { useCancelDeployment, useRedeploy } from "../../api/deployments";
import { isPending, type Deployment } from "../../api/types";
import { ErrorText } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Menu, MenuItem, MenuSeparator } from "../../components/Menu";
import { RelativeTime } from "../../components/RelativeTime";
import { StatusDot } from "../../components/StatusDot";
import { formatDuration } from "../../lib/time";
import { useRedeployHint } from "../services/redeploy";
import { DeploymentSummary } from "./DeploymentSummary";
import styles from "./DeploymentList.module.css";

type DeploymentListProps = {
  serviceId: string;
  deployments: Deployment[];
  onViewLogs: (id: string) => void;
};

export function DeploymentList({ serviceId, deployments, onViewLogs }: DeploymentListProps) {
  const redeploy = useRedeploy(serviceId);
  const cancel = useCancelDeployment(serviceId);
  const hint = useRedeployHint();

  return (
    <div className={styles.wrap}>
      <ErrorText error={redeploy.error ?? cancel.error} />
      <ul className={styles.list}>
        {deployments.map((d) => (
          <li key={d.id} className={styles.row} data-active={d.status === "active" || undefined}>
            <button
              type="button"
              className={styles.open}
              onClick={() => onViewLogs(d.id)}
              aria-label="View build logs"
            />
            <div className={styles.status}>
              <StatusDot status={d.status} />
            </div>
            <div className={styles.main}>
              <DeploymentSummary deployment={d} />
              {d.error && <p className={styles.error}>{d.error}</p>}
            </div>
            <div className={styles.time}>
              <RelativeTime iso={d.createdAt} />
              {d.startedAt && (
                <span className={styles.duration}>{formatDuration(d.startedAt, d.finishedAt)}</span>
              )}
            </div>
            <Menu
              trigger={
                <Button
                  variant="ghost"
                  size="sm"
                  icon
                  className={styles.menu}
                  aria-label="Deployment actions"
                >
                  <Ellipsis size={16} />
                </Button>
              }
            >
              <MenuItem icon={<FileText size={14} />} onClick={() => onViewLogs(d.id)}>
                View logs
              </MenuItem>
              <MenuItem
                icon={<RotateCcw size={14} />}
                disabled={!d.image}
                onClick={() => redeploy.mutate(d.id, { onSuccess: hint.clear })}
              >
                Redeploy
              </MenuItem>
              {isPending(d.status) && (
                <>
                  <MenuSeparator />
                  <MenuItem icon={<Ban size={14} />} danger onClick={() => cancel.mutate(d.id)}>
                    Cancel
                  </MenuItem>
                </>
              )}
            </Menu>
          </li>
        ))}
      </ul>
    </div>
  );
}
