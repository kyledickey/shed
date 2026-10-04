import {
  Archive,
  Check,
  Download,
  Ellipsis,
  Hourglass,
  Lock,
  RotateCcw,
  Trash2,
  TriangleAlert,
  X,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { downloadUrl, useDeleteBackup, useRestoreBackup, useRunBackup } from "../../api/backups";
import { errorMessage } from "../../api/client";
import {
  isBackupActive,
  type Backup,
  type BackupMethod,
  type BackupStatus,
  type BackupTrigger,
} from "../../api/types";
import { Badge, Count, Spinner } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { List } from "../../components/Layout";
import { EmptyState } from "../../components/Misc";
import { Menu, MenuItem, MenuSeparator, Tooltip, useToast } from "../../components/Overlay";
import type { Tone } from "../../components/tone";
import { formatBytes } from "../../lib/format";
import { formatDate, formatDuration, relativeTime } from "../../lib/time";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import styles from "./Backups.module.css";

/** The API returns at most this many backups. */
const LIST_LIMIT = 100;

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
  succeeded: { tone: "grass", label: "Succeeded", live: false },
  failed: { tone: "tomato", label: "Failed", live: false },
};

type BackupListProps = {
  /** A service id, or null for shed's own database, which cannot be restored from the dashboard. */
  scope: string | null;
  serviceName?: string;
  backups: Backup[];
  /** A restore is running for this service, so another cannot start. */
  restoreRunning?: boolean;
};

/** BackupList lists backups, newest first, with a "Back up now" button and per-row actions. */
export function BackupList({ scope, serviceName, backups, restoreRunning }: BackupListProps) {
  const [restoring, setRestoring] = useState<Backup | null>(null);
  const [deleting, setDeleting] = useState<Backup | null>(null);

  return (
    <LayerCard
      title="Backups"
      meta={<Count>{backups.length}</Count>}
      actions={<BackupNowButton scope={scope} busy={backups.some(isBackupActive)} />}
      sheetClassName={styles.sheet}
      footer={
        backups.length >= LIST_LIMIT && (
          <span className={styles.foot}>Showing the {LIST_LIMIT} most recent backups.</span>
        )
      }
    >
      {backups.length === 0 ? (
        <EmptyState
          icon={<Archive />}
          tone="neutral"
          title="No backups yet"
          description="Backups appear here when the schedule runs or when you back up now."
        />
      ) : (
        <List>
          {backups.map((b) => (
            <BackupRow
              key={b.id}
              backup={b}
              onRestore={scope && !restoreRunning ? () => setRestoring(b) : undefined}
              onDelete={() => setDeleting(b)}
            />
          ))}
        </List>
      )}
      {scope && (
        <RestoreDialog
          serviceId={scope}
          serviceName={serviceName}
          backup={restoring}
          onClose={() => setRestoring(null)}
        />
      )}
      <DeleteDialog scope={scope} backup={deleting} onClose={() => setDeleting(null)} />
    </LayerCard>
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

function BackupRow({
  backup: b,
  onRestore,
  onDelete,
}: {
  backup: Backup;
  onRestore?: () => void;
  onDelete: () => void;
}) {
  const look = statusLooks[b.status];
  const active = isBackupActive(b);
  const stored = b.local || b.remote;
  return (
    <div className={styles.row}>
      <div className={styles.rowMain}>
        <span className={styles.rail} data-tone={look.tone}>
          <StatusGlyph status={b.status} />
        </span>
        <div className={styles.body}>
          <span className={styles.title}>
            {formatDate(b.createdAt)}
            <span className={styles.ago}>{relativeTime(b.createdAt)}</span>
          </span>
          <span className={styles.meta}>
            <Badge size="sm" variant={b.trigger === "pre-restore" ? "outline" : "soft"}>
              {triggerLabels[b.trigger]}
            </Badge>
            <span>{methodLabels[b.method]}</span>
            {b.status === "succeeded" && <span>{formatBytes(b.size)}</span>}
            {b.finishedAt && <span>{formatDuration(b.createdAt, b.finishedAt)}</span>}
            {b.fileName && <span className={styles.file}>{b.fileName}</span>}
          </span>
          {b.error && (
            <Problem kind="error" icon={<X size={12} />}>
              {b.error}
            </Problem>
          )}
          {b.remoteError && (
            <Problem kind="warning" icon={<TriangleAlert size={12} />}>
              Upload to S3 failed: {b.remoteError}
            </Problem>
          )}
        </div>
        <div className={styles.side}>
          {b.status === "succeeded" ? (
            <span className={styles.badges}>
              {b.encrypted && (
                <Tooltip content="Encrypted with age">
                  <span className={styles.lock} aria-label="Encrypted">
                    <Lock size={13} />
                  </span>
                </Tooltip>
              )}
              {b.local && (
                <Badge size="sm" variant="outline">
                  Local
                </Badge>
              )}
              {b.remote && (
                <Badge size="sm" tone="accent">
                  S3
                </Badge>
              )}
              {!stored && (
                <Badge size="sm" variant="outline">
                  No copy
                </Badge>
              )}
            </span>
          ) : (
            <Badge size="sm" tone={look.tone}>
              {look.live && <Spinner tone={look.tone} size={8} />}
              {look.label}
            </Badge>
          )}
        </div>
      </div>
      <div className={styles.actions}>
        <Menu
          trigger={
            <Button variant="ghost" size="sm" icon aria-label="Backup actions">
              <Ellipsis size={14} />
            </Button>
          }
        >
          {b.status === "succeeded" && stored && (
            <MenuItem icon={<Download size={14} />} href={downloadUrl(b.id)} download>
              Download
            </MenuItem>
          )}
          {onRestore && b.status === "succeeded" && stored && (
            <MenuItem icon={<RotateCcw size={14} />} onClick={onRestore}>
              Restore…
            </MenuItem>
          )}
          {b.status === "succeeded" && stored && <MenuSeparator />}
          <MenuItem icon={<Trash2 size={14} />} danger disabled={active} onClick={onDelete}>
            Delete
          </MenuItem>
        </Menu>
      </div>
    </div>
  );
}

function StatusGlyph({ status }: { status: BackupStatus }) {
  switch (status) {
    case "succeeded":
      return <Check size={13} strokeWidth={2.5} />;
    case "failed":
      return <X size={13} strokeWidth={2.5} />;
    case "running":
      return <Spinner tone="sky" size={12} />;
    default:
      return <Hourglass size={12} />;
  }
}

function Problem({
  kind,
  icon,
  children,
}: {
  kind: "error" | "warning";
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <p className={styles.problem} data-kind={kind}>
      {icon}
      <span>{children}</span>
    </p>
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
