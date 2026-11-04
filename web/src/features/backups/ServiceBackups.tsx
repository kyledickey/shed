import { buttonVariants } from "@cloudflare/kumo";
import { CheckCircleIcon, XCircleIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { backupSettingsQuery, serviceBackupsQuery } from "../../api/backups";
import { useCursorPages } from "../../api/paging";
import { isBackupActive, type Backup, type Restore, type Service } from "../../api/types";
import { Spinner } from "../../components/Badge";
import { Notice } from "../../components/Notice";
import { formatDate, relativeTime } from "../../lib/time";
import { BackupList } from "./BackupList";
import { PolicyCard } from "./PolicyCard";
import styles from "./ServiceBackups.module.css";

/** Finished restores stay on screen this long. */
const SUCCEEDED_BANNER_MS = 60 * 60 * 1000;
const FAILED_BANNER_MS = 24 * 60 * 60 * 1000;

const description = (service: Service) =>
  service.kind === "app"
    ? "shed archives this service's volumes while it runs, so files that are being written can be captured mid-change."
    : "While the database is running, shed takes a logical dump of it. While it is stopped, it archives the volume files instead.";

/** ServiceBackups is the Backups tab of a service that has volumes. */
export function ServiceBackups({ service }: { service: Service }) {
  const pages = useCursorPages();
  // The first page holds the policy, the latest restore, and any backup in progress.
  const { data } = useSuspenseQuery(serviceBackupsQuery(service.id));
  const { data: page = data, isPlaceholderData } = useQuery(
    serviceBackupsQuery(service.id, pages.before),
  );
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);

  if (service.volumes.length === 0) {
    return (
      <Notice
        title="No volumes to back up"
        actions={
          <Link
            className={buttonVariants({ size: "sm" })}
            to="/projects/$projectId/services/$serviceId/settings"
            params={{ projectId: service.projectId, serviceId: service.id }}
          >
            Open settings
          </Link>
        }
      >
        Only services with a volume can be backed up. Add one in the service settings.
      </Notice>
    );
  }

  return (
    <div className={styles.page}>
      <p className={styles.intro}>{description(service)}</p>
      {data.restore && (
        <RestoreBanner restore={data.restore} backups={[...data.backups, ...page.backups]} />
      )}
      <PolicyCard
        key={service.id}
        scope={service.id}
        policy={data.policy}
        s3Configured={settings.s3 !== null}
      />
      <BackupList
        scope={service.id}
        serviceName={service.name}
        backups={page.backups}
        next={page.next}
        pages={pages}
        turning={isPlaceholderData}
        backingUp={data.backups.some(isBackupActive)}
        restoreRunning={data.restore?.status === "running"}
      />
    </div>
  );
}

function RestoreBanner({ restore, backups }: { restore: Restore; backups: Backup[] }) {
  const source = backups.find((b) => b.id === restore.backupId);
  const from = source ? formatDate(source.createdAt) : "a backup";
  const finished = restore.finishedAt ? Date.now() - new Date(restore.finishedAt).getTime() : 0;

  switch (restore.status) {
    case "running":
      return (
        <Notice icon={<Spinner tone="sky" size={14} />} title={`Restoring from ${from}`}>
          The service is held until the restore finishes. Deploys and restarts are rejected in the
          meantime.
        </Notice>
      );
    case "failed":
      if (finished > FAILED_BANNER_MS) return null;
      return (
        <Notice icon={<XCircleIcon size={16} />} title={`Restore from ${from} failed`} danger>
          {restore.error || "The restore failed."}
        </Notice>
      );
    case "succeeded":
      if (finished > SUCCEEDED_BANNER_MS) return null;
      return (
        <Notice icon={<CheckCircleIcon size={16} />} title={`Restored from ${from}`}>
          Finished {restore.finishedAt && relativeTime(restore.finishedAt)}.
        </Notice>
      );
  }
}
