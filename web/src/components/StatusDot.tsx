import type { DeploymentStatus, ServiceStatus } from "../api/types";
import styles from "./StatusDot.module.css";

type Status = ServiceStatus | DeploymentStatus;
type Tone = "green" | "amber" | "red" | "gray";

const tones: Record<Status, Tone> = {
  active: "green",
  deploying: "amber",
  queued: "amber",
  waiting: "amber",
  building: "amber",
  failed: "red",
  crashed: "red",
  offline: "gray",
  removed: "gray",
  canceled: "gray",
  skipped: "gray",
};

const labels: Partial<Record<Status, string>> = {
  waiting: "Waiting for CI",
  skipped: "Skipped",
};

function statusLabel(status: Status): string {
  return labels[status] ?? status.charAt(0).toUpperCase() + status.slice(1);
}

type StatusDotProps = { status: Status; label?: boolean };

export function StatusDot({ status, label = true }: StatusDotProps) {
  const tone = tones[status];
  return (
    <span className={styles.status} title={label ? undefined : statusLabel(status)}>
      <span className={styles.dot} data-tone={tone} aria-hidden />
      {label ? (
        <span>{statusLabel(status)}</span>
      ) : (
        <span className={styles.srOnly}>{statusLabel(status)}</span>
      )}
    </span>
  );
}
