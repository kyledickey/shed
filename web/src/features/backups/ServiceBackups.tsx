import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CircleCheck, CircleX, HardDrive } from "lucide-react";
import { backupSettingsQuery, serviceBackupsQuery } from "../../api/backups";
import type { Backup, Restore, Service } from "../../api/types";
import { Spinner } from "../../components/Badge";
import { buttonClass } from "../../components/Button";
import { Section, Stack } from "../../components/Layout";
import { Callout, EmptyState } from "../../components/Misc";
import { formatDate, relativeTime } from "../../lib/time";
import { BackupList } from "./BackupList";
import { PolicyCard } from "./PolicyCard";

/** Finished restores stay on screen this long. */
const SUCCEEDED_BANNER_MS = 60 * 60 * 1000;
const FAILED_BANNER_MS = 24 * 60 * 60 * 1000;

const description = (service: Service) =>
  service.kind === "app"
    ? "shed archives this service's volumes while it runs, so files that are being written can be captured mid-change."
    : "While the database is running, shed takes a logical dump of it. While it is stopped, it archives the volume files instead.";

/** ServiceBackups is the Backups tab of a service that has volumes. */
export function ServiceBackups({ service }: { service: Service }) {
  const { data } = useSuspenseQuery(serviceBackupsQuery(service.id));
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);

  if (service.volumes.length === 0) {
    return (
      <EmptyState
        icon={<HardDrive />}
        tone="neutral"
        title="No volumes to back up"
        description="Only services with a volume can be backed up. Add one in the service settings."
        actions={
          <Link
            className={buttonClass()}
            to="/projects/$projectId/services/$serviceId/settings"
            params={{ projectId: service.projectId, serviceId: service.id }}
          >
            Open settings
          </Link>
        }
      />
    );
  }

  return (
    <Section title="Backups" description={description(service)}>
      <Stack gap={4}>
        {data.restore && <RestoreBanner restore={data.restore} backups={data.backups} />}
        <PolicyCard
          key={service.id}
          scope={service.id}
          policy={data.policy}
          s3Configured={settings.s3 !== null}
        />
        <BackupList
          scope={service.id}
          serviceName={service.name}
          backups={data.backups}
          restoreRunning={data.restore?.status === "running"}
        />
      </Stack>
    </Section>
  );
}

function RestoreBanner({ restore, backups }: { restore: Restore; backups: Backup[] }) {
  const source = backups.find((b) => b.id === restore.backupId);
  const from = source ? formatDate(source.createdAt) : "a backup";
  const finished = restore.finishedAt ? Date.now() - new Date(restore.finishedAt).getTime() : 0;

  switch (restore.status) {
    case "running":
      return (
        <Callout
          tone="sky"
          icon={<Spinner tone="sky" size={14} />}
          title={`Restoring from ${from}`}
        >
          The service is held until the restore finishes. Deploys and restarts are rejected in the
          meantime.
        </Callout>
      );
    case "failed":
      if (finished > FAILED_BANNER_MS) return null;
      return (
        <Callout tone="tomato" icon={<CircleX size={16} />} title={`Restore from ${from} failed`}>
          {restore.error || "The restore failed."}
        </Callout>
      );
    case "succeeded":
      if (finished > SUCCEEDED_BANNER_MS) return null;
      return (
        <Callout tone="grass" icon={<CircleCheck size={16} />} title={`Restored from ${from}`}>
          Finished {restore.finishedAt && relativeTime(restore.finishedAt)}.
        </Callout>
      );
  }
}
