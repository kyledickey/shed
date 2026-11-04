import { useState } from "react";
import { Button, DropdownMenu, Tooltip } from "@cloudflare/kumo";
import {
  ArrowCounterClockwiseIcon,
  DotsThreeIcon,
  DownloadSimpleIcon,
  LockSimpleIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { downloadUrl, useDeleteBackup, useRestoreBackup, useRunBackup } from "../../api/backups";
import { errorMessage } from "../../api/client";
import type { CursorPages } from "../../api/paging";
import {
  isBackupActive,
  type Backup,
  type BackupMethod,
  type BackupStatus,
  type BackupTrigger,
} from "../../api/types";
import { Spinner } from "../../components/Badge";
import { useToast } from "../../components/Overlay";
import { Pager } from "../../components/Pager";
import { Pill } from "../../components/Pill";
import {
  RecordEmpty,
  RecordEnd,
  RecordList,
  RecordMessage,
  RecordMeta,
  RecordNote,
  RecordRow,
  RecordText,
} from "../../components/RecordList";
import type { Tone } from "../../components/tone";
import { formatBytes } from "../../lib/format";
import { formatDate, formatDuration, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import styles from "./BackupList.module.css";

const COLUMNS = "minmax(150px, 170px) minmax(0, 1fr) 112px 100px minmax(0, 170px) 124px 96px 32px";
const NARROW = "minmax(0, 1fr) auto 32px";

const triggerLabels: Record<BackupTrigger, string> = {
  schedule: "Scheduled",
  manual: "Manual",
  "pre-restore": "Pre-restore",
};

const methodLabels: Record<BackupMethod, string> = {
  dump: "Dump",
  volume: "Volume",
  sqlite: "SQLite",
};

const statusLooks: Record<BackupStatus, { tone: Tone; label: string; live: boolean }> = {
  queued: { tone: "neutral", label: "Queued", live: true },
  running: { tone: "sky", label: "Running", live: true },
  uploading: { tone: "sky", label: "Uploading", live: true },
  succeeded: { tone: "grass", label: "Succeeded", live: false },
  failed: { tone: "tomato", label: "Failed", live: false },
};

type BackupListProps = {
  /** A service id, or null for shed's own database, which cannot be restored from the dashboard. */
  scope: string | null;
  serviceName?: string;
  /** The page of backups on screen. */
  backups: Backup[];
  /** The cursor of the following page, or null on the last. */
  next: string | null;
  pages: CursorPages;
  /** The next page is loading. */
  turning?: boolean;
  /** A backup is queued, running, or uploading; it is on the first page. */
  backingUp: boolean;
  /** A restore is running for this service, so another cannot start. */
  restoreRunning?: boolean;
};

/** BackupList lists backups, newest first, a page at a time, with a "Back up now" button and per-row actions. */
export function BackupList({
  scope,
  serviceName,
  backups,
  next,
  pages,
  turning,
  backingUp,
  restoreRunning,
}: BackupListProps) {
  const [restoring, setRestoring] = useState<Backup | null>(null);
  const [deleting, setDeleting] = useState<Backup | null>(null);

  return (
    <>
      <RecordList
        title={
          <>
            <HelpTip topic="backupMethods">Backups</HelpTip>
          </>
        }
        actions={<BackupNowButton scope={scope} busy={backingUp} />}
        footer={
          (pages.hasPrevious || next) && (
            <Pager
              count={backups.length}
              hasPrevious={pages.hasPrevious}
              hasNext={next !== null}
              busy={turning}
              onPrevious={pages.previous}
              onNext={() => next && pages.next(next)}
            />
          )
        }
      >
        {backups.length === 0 ? (
          <RecordEmpty>
            {pages.hasPrevious
              ? "No older backups."
              : "No backups yet. They appear here when the schedule runs or when you back up now."}
          </RecordEmpty>
        ) : (
          backups.map((b) => (
            <BackupRow
              key={b.id}
              backup={b}
              onRestore={scope && !restoreRunning ? () => setRestoring(b) : undefined}
              onDelete={() => setDeleting(b)}
            />
          ))
        )}
      </RecordList>
      {scope && (
        <RestoreDialog
          serviceId={scope}
          serviceName={serviceName}
          backup={restoring}
          onClose={() => setRestoring(null)}
        />
      )}
      <DeleteDialog scope={scope} backup={deleting} onClose={() => setDeleting(null)} />
    </>
  );
}

function BackupNowButton({ scope, busy }: { scope: string | null; busy: boolean }) {
  const run = useRunBackup(scope);
  const toast = useToast();
  const working = busy || run.isPending;
  return (
    <Button
      size="sm"
      loading={working}
      onClick={() =>
        run.mutate(undefined, {
          onSuccess: () => toast.add({ type: "info", title: "Backup queued" }),
          onError: (err) =>
            toast.add({
              type: "error",
              title: "Couldn't start backup",
              description: errorMessage(err),
            }),
        })
      }
    >
      {working ? "Backing up…" : "Back up now"}
    </Button>
  );
}

/** BackupStatusWord is a backup's status as a thin bar and a word, or a spinner while it runs. */
function BackupStatusWord({ status }: { status: BackupStatus }) {
  const look = statusLooks[status];
  return (
    <span className={styles.status} data-tone={look.tone}>
      {look.live ? <Spinner tone={look.tone} size={10} /> : <span className={styles.bar} />}
      {look.label}
    </span>
  );
}

function BackupRow({
  backup: b,
  onRestore,
  onDelete,
}: {
  backup: Backup;
  onRestore?: () => void;
  onDelete: () => void;
}) {
  const active = isBackupActive(b);
  const stored = b.local || b.remote;
  const uploadFailed = b.remoteError !== "";
  const done = b.status === "succeeded";
  const details = [
    methodLabels[b.method],
    (done || b.status === "uploading") && formatBytes(b.size),
    b.finishedAt && formatDuration(b.createdAt, b.finishedAt),
  ].filter(Boolean);
  return (
    <RecordRow columns={COLUMNS} narrowColumns={NARROW}>
      <RecordText title={relativeTime(b.createdAt)}>{formatDate(b.createdAt)}</RecordText>
      <RecordMessage optional className={styles.file} title={b.fileName}>
        {b.fileName}
      </RecordMessage>
      <span className={styles.statusCell}>
        <BackupStatusWord status={b.status} />
      </span>
      <RecordMeta optional>
        <Pill>{triggerLabels[b.trigger]}</Pill>
      </RecordMeta>
      <RecordMeta optional>{details.join(" · ")}</RecordMeta>
      <RecordMeta optional className={styles.stored}>
        {done && b.local && <Pill>Local</Pill>}
        {done && b.remote && !uploadFailed && <Pill>S3</Pill>}
        {done && uploadFailed && (
          <Tooltip
            content={`Upload to S3 failed: ${b.remoteError}`}
            render={
              <span className={styles.failedPill} aria-label="Upload to S3 failed" tabIndex={0}>
                <Pill>S3</Pill>
              </span>
            }
          />
        )}
        {done && !stored && !uploadFailed && "No copy"}
        {done && b.encrypted && (
          <Tooltip
            content="Encrypted with age"
            render={
              <span className={styles.lock} aria-label="Encrypted" tabIndex={0}>
                <LockSimpleIcon size={14} />
              </span>
            }
          />
        )}
      </RecordMeta>
      <RecordMeta optional className={styles.when}>
        {relativeTime(b.createdAt)}
      </RecordMeta>
      <RecordEnd>
        <DropdownMenu>
          <DropdownMenu.Trigger
            render={
              <Button
                variant="ghost"
                size="sm"
                shape="square"
                icon={<DotsThreeIcon size={16} weight="bold" />}
                aria-label="Backup actions"
              />
            }
          />
          <DropdownMenu.Content align="end">
            {done && stored && (
              <DropdownMenu.LinkItem
                icon={DownloadSimpleIcon}
                href={downloadUrl(b.id)}
                render={<a download />}
              >
                Download
              </DropdownMenu.LinkItem>
            )}
            {onRestore && done && stored && (
              <DropdownMenu.Item icon={ArrowCounterClockwiseIcon} onClick={onRestore}>
                Restore…
              </DropdownMenu.Item>
            )}
            {done && stored && <DropdownMenu.Separator />}
            <DropdownMenu.Item
              icon={TrashIcon}
              variant="danger"
              disabled={active}
              onClick={onDelete}
            >
              Delete
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu>
      </RecordEnd>
      {b.error && <RecordNote className={styles.error}>{b.error}</RecordNote>}
    </RecordRow>
  );
}

function RestoreDialog({
  serviceId,
  serviceName,
  backup,
  onClose,
}: {
  serviceId: string;
  serviceName?: string;
  backup: Backup | null;
  onClose: () => void;
}) {
  const restore = useRestoreBackup(serviceId);
  const toast = useToast();
  return (
    <ConfirmDialog
      open={!!backup}
      onOpenChange={(open) => {
        if (!open) onClose();
        restore.reset();
      }}
      title="Restore backup"
      confirmLabel="Restore backup"
      pending={restore.isPending}
      error={restore.error ? errorMessage(restore.error) : undefined}
      onConfirm={() =>
        backup &&
        restore.mutate(backup.id, {
          onSuccess: () => {
            toast.add({ type: "info", title: "Restore started", description: serviceName });
            onClose();
          },
        })
      }
    >
      Restoring replaces the current data of <strong>{serviceName ?? "this service"}</strong> with
      the backup from <strong>{backup && formatDate(backup.createdAt)}</strong>.
      <ul className={styles.bullets}>
        <li>
          A pre-restore backup of the current data is taken first. If it fails, nothing is restored.
        </li>
        <li>
          The service is held for the whole restore: deploys, starts, stops, and restarts are
          rejected, and deployments in progress are canceled.
        </li>
        <li>
          Its container may be stopped while data is replaced. It starts again afterwards, unless
          you had stopped it.
        </li>
        <li>
          The current data is overwritten and can only be recovered from the pre-restore backup.
        </li>
      </ul>
    </ConfirmDialog>
  );
}

function DeleteDialog({
  scope,
  backup,
  onClose,
}: {
  scope: string | null;
  backup: Backup | null;
  onClose: () => void;
}) {
  const remove = useDeleteBackup(scope);
  const toast = useToast();
  return (
    <ConfirmDialog
      open={!!backup}
      onOpenChange={(open) => {
        if (!open) onClose();
        remove.reset();
      }}
      title="Delete backup"
      confirmLabel="Delete backup"
      pending={remove.isPending}
      error={remove.error ? errorMessage(remove.error) : undefined}
      onConfirm={() =>
        backup &&
        remove.mutate(backup.id, {
          onSuccess: () => {
            toast.add({ type: "success", title: "Backup deleted" });
            onClose();
          },
        })
      }
    >
      The backup from <strong>{backup && formatDate(backup.createdAt)}</strong> will be deleted,
      including its local file and its S3 object. This can't be undone.
    </ConfirmDialog>
  );
}
