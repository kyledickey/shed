import { Banner, Button, Loader, Select, Switch } from "@cloudflare/kumo";
import {
  ArrowClockwiseIcon,
  ArrowUpRightIcon,
  DownloadSimpleIcon,
  WarningIcon,
  CaretRightIcon,
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import type { UpdateChannel, UpdateStatus } from "../../api/types";
import {
  updateQuery,
  useCheckUpdate,
  useDownloadUpdate,
  useInstallUpdate,
  useSetAutoDownload,
  useSetUpdateChannel,
} from "../../api/update";
import { Rows, SettingRow, Well } from "../../components/Settings";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { useBeginRestart } from "../update/restart";
import { ConfirmDialog } from "./ConfirmDialog";

import form from "./Form.module.css";
import { ReleaseNotes } from "./ReleaseNotes";
import styles from "./UpdatesCard.module.css";

/** UpdatesCard shows the running and latest shed versions and drives download and install. */
export function UpdatesCard() {
  const status = useQuery(updateQuery);
  const check = useCheckUpdate();
  const download = useDownloadUpdate();
  const autoDownload = useSetAutoDownload();
  const channel = useSetUpdateChannel();
  const install = useInstallUpdate();
  const beginRestart = useBeginRestart();

  const actionError = [check, download, autoDownload, channel].find((m) => m.error)?.error;
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
      onChannel={(ch) => channel.mutate(ch)}
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
  onChannel: (channel: UpdateChannel) => void;
  onInstall: (version: string) => void;
  onCloseInstall: () => void;
};

const channels: { value: UpdateChannel; label: string }[] = [
  { value: "stable", label: "Stable" },
  { value: "nightly", label: "Nightly" },
];

/** UpdatesCardView renders the update settings from a status; UpdatesCard wires it to the API. */
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
  onChannel,
  onInstall,
  onCloseInstall,
}: ViewProps) {
  const [confirming, setConfirming] = useState(false);

  if (!status) {
    return loadError ? (
      <Banner variant="error" icon={<WarningIcon weight="fill" />} description={loadError} />
    ) : (
      <Well empty>
        <Loader size="sm" />
      </Well>
    );
  }

  const { latest, unsupported } = status;
  const busyChecking = checking || status.state === "checking";
  const busyDownloading = downloading || status.state === "downloading";
  const error = actionError || status.error;
  const showNotes = !!latest?.notes && (status.available || !!status.staged);

  return (
    <Rows>
      {error && <Banner variant="error" icon={<WarningIcon weight="fill" />} description={error} />}
      <SettingRow
        label={
          <>
            <HelpTip topic="updates">Running version</HelpTip>
          </>
        }
        description={
          unsupported ||
          (status.checkedAt ? (
            <span title={formatDate(status.checkedAt)}>
              Last checked {relativeTime(status.checkedAt)}.
            </span>
          ) : (
            "Never checked for updates."
          ))
        }
        control={
          <>
            <span className={form.value}>{status.current}</span>
            <Button
              icon={busyChecking ? undefined : <ArrowClockwiseIcon />}
              loading={busyChecking}
              disabled={!!unsupported || busyDownloading}
              onClick={onCheck}
            >
              Check now
            </Button>
          </>
        }
      />
      {!unsupported && (
        <SettingRow
          label="Latest release"
          description={
            latest ? (
              <>
                {status.staged
                  ? `${status.staged} is downloaded and verified. `
                  : status.available
                    ? `${latest.version} is available. `
                    : "shed is up to date. "}
                <span>Published {formatDate(latest.publishedAt)} · </span>
                <a
                  className={form.link}
                  href={latest.url}
                  target="_blank"
                  rel="noreferrer noopener"
                >
                  Release <ArrowUpRightIcon size={12} />
                </a>
              </>
            ) : (
              "Not checked yet."
            )
          }
          control={
            status.staged ? (
              <Button variant="primary" onClick={() => setConfirming(true)}>
                Install {status.staged} and restart
              </Button>
            ) : busyDownloading ? (
              <Button variant="primary" loading disabled>
                Downloading…
              </Button>
            ) : status.available && latest ? (
              <Button variant="primary" icon={<DownloadSimpleIcon />} onClick={onDownload}>
                Download {latest.version}
              </Button>
            ) : (
              latest && <span className={form.value}>{latest.version}</span>
            )
          }
        >
          {showNotes && latest && (
            <Well>
              <details className={styles.notes}>
                <summary>
                  <CaretRightIcon size={12} className={styles.caret} />
                  Release notes for {latest.version}
                </summary>
                <ReleaseNotes notes={latest.notes} />
              </details>
            </Well>
          )}
        </SettingRow>
      )}
      {!unsupported && (
        <SettingRow
          label={
            <>
              <HelpTip topic="updateChannel">Channel</HelpTip>
            </>
          }
          description="Nightlies are untested daily builds of main. Switching back to stable waits for the next stable release; shed never downgrades."
          control={
            <Select
              aria-label="Update channel"
              className={styles.channel}
              value={status.channel}
              onValueChange={(v) => v && onChannel(v)}
              items={channels}
            />
          }
        />
      )}
      <SettingRow
        label="Download updates automatically"
        description="New releases are downloaded and verified as soon as they are found. Installing is always manual."
        control={
          <Switch
            aria-label="Download updates automatically"
            checked={status.autoDownload}
            disabled={!!unsupported}
            onCheckedChange={onAutoDownload}
          />
        }
      />

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
    </Rows>
  );
}
