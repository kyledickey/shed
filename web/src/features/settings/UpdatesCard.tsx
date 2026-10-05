import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, CircleCheck, Download, RefreshCw } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import type { UpdateStatus } from "../../api/types";
import {
  updateQuery,
  useCheckUpdate,
  useDownloadUpdate,
  useInstallUpdate,
  useSetAutoDownload,
} from "../../api/update";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Switch } from "../../components/Form";
import { Skeleton } from "../../components/Misc";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { useBeginRestart } from "../update/restart";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./UpdatesCard.module.css";

/** UpdatesCard shows the running and latest shed versions and drives download and install. */
export function UpdatesCard() {
  const status = useQuery(updateQuery);
  const check = useCheckUpdate();
  const download = useDownloadUpdate();
  const autoDownload = useSetAutoDownload();
  const install = useInstallUpdate();
  const beginRestart = useBeginRestart();

  const actionError = [check, download, autoDownload].find((m) => m.error)?.error;
  return (
    <UpdatesCardView
      status={status.data}
      loadError={status.error ? errorMessage(status.error) : undefined}
      actionError={actionError ? errorMessage(actionError) : undefined}
      installError={install.error ? errorMessage(install.error) : undefined}
      checking={check.isPending}
      downloading={download.isPending}
      installing={install.isPending}
      onCheck={() => check.mutate()}
      onDownload={() => download.mutate()}
      onAutoDownload={(on) => autoDownload.mutate(on)}
      onInstall={(version) => install.mutate(undefined, { onSuccess: () => beginRestart(version) })}
      onCloseInstall={() => install.reset()}
    />
  );
}

type ViewProps = {
  status: UpdateStatus | undefined;
  loadError?: string;
  actionError?: string;
  installError?: string;
  checking: boolean;
  downloading: boolean;
  installing: boolean;
  onCheck: () => void;
  onDownload: () => void;
  onAutoDownload: (on: boolean) => void;
  onInstall: (version: string) => void;
  onCloseInstall: () => void;
};

/** UpdatesCardView renders the update card from a status; UpdatesCard wires it to the API. */
export function UpdatesCardView({
  status,
  loadError,
  actionError,
  installError,
  checking,
  downloading,
  installing,
  onCheck,
  onDownload,
  onAutoDownload,
  onInstall,
  onCloseInstall,
}: ViewProps) {
  const [confirming, setConfirming] = useState(false);
  const title = (
    <>
      Updates <HelpTip topic="updates" />
    </>
  );

  if (!status) {
    return (
      <LayerCard title={title} meta="Keep shed up to date." padded>
        {loadError ? (
          <p className={styles.error}>{loadError}</p>
        ) : (
          <Skeleton width="60%" height={14} />
        )}
      </LayerCard>
    );
  }

  const { latest, unsupported } = status;
  const busyChecking = checking || status.state === "checking";
  const busyDownloading = downloading || status.state === "downloading";
  const error = actionError || status.error;

  return (
    <LayerCard
      title={title}
      meta="Keep shed up to date."
      padded
      actions={
        <Button
          size="sm"
          loading={busyChecking}
          disabled={!!unsupported || busyDownloading}
          onClick={onCheck}
        >
          {!busyChecking && <RefreshCw size={13} />} Check now
        </Button>
      }
    >
      <div className={styles.body}>
        <dl className={styles.facts}>
          <div>
            <dt>Running</dt>
            <dd className={styles.mono}>{status.current}</dd>
          </div>
          <div>
            <dt>Latest</dt>
            <dd>
              {latest ? (
                <>
                  <span className={styles.mono}>{latest.version}</span>
                  <span className={styles.muted}> · {formatDate(latest.publishedAt)}</span>
                  <a
                    className={styles.link}
                    href={latest.url}
                    target="_blank"
                    rel="noreferrer noopener"
                  >
                    Release <ArrowUpRight size={12} />
                  </a>
                </>
              ) : (
                <span className={styles.muted}>Not checked yet</span>
              )}
            </dd>
          </div>
          <div>
            <dt>Last checked</dt>
            <dd>
              {status.checkedAt ? (
                <span title={formatDate(status.checkedAt)}>{relativeTime(status.checkedAt)}</span>
              ) : (
                <span className={styles.muted}>Never</span>
              )}
            </dd>
          </div>
        </dl>

        <div className={styles.action}>
          <div className={styles.state}>
            {unsupported ? (
              <p className={styles.muted}>{unsupported}</p>
            ) : status.staged ? (
              <p>
                <span className={styles.mono}>{status.staged}</span> is downloaded and verified.
              </p>
            ) : status.available && latest ? (
              <p>
                <span className={styles.mono}>{latest.version}</span> is available.
              </p>
            ) : (
              <p className={styles.upToDate}>
                <CircleCheck size={15} /> shed is up to date
              </p>
            )}
          </div>
          {!unsupported && status.staged ? (
            <Button variant="primary" onClick={() => setConfirming(true)}>
              Install {status.staged} and restart
            </Button>
          ) : !unsupported && busyDownloading ? (
            <Button variant="primary" loading disabled>
              Downloading…
            </Button>
          ) : !unsupported && status.available && latest ? (
            <Button variant="primary" onClick={onDownload}>
              <Download size={14} /> Download {latest.version}
            </Button>
          ) : null}
        </div>

        {error && <p className={styles.error}>{error}</p>}

        <Switch
          checked={status.autoDownload}
          onChange={onAutoDownload}
          disabled={!!unsupported}
          label="Download updates automatically"
          description="New releases are downloaded and verified as soon as they are found. Installing is always manual."
        />

        {latest?.notes && (status.available || status.staged) && (
          <details className={styles.notes}>
            <summary>Release notes for {latest.version}</summary>
            <pre>{latest.notes}</pre>
          </details>
        )}
      </div>

      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          setConfirming(open);
          if (!open) onCloseInstall();
        }}
        title={`Install ${status.staged} and restart?`}
        confirmLabel="Install and restart"
        confirmVariant="primary"
        pending={installing}
        error={installError}
        onConfirm={() => onInstall(status.staged)}
      >
        shed restarts into the new version. Containers keep running, but every domain and the
        dashboard are unreachable for a few seconds. Running builds, backups, and restores are
        interrupted.
      </ConfirmDialog>
    </LayerCard>
  );
}
