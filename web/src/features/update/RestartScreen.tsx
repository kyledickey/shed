import { Dialog } from "@base-ui/react/dialog";
import { TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { Spinner } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Callout, CopyButton } from "../../components/Misc";
import { hasRestarted, restartAgain, timeoutMs, useRestart } from "./restart";
import styles from "./RestartScreen.module.css";

const POLL_MS = 1500;
const REQUEST_TIMEOUT_MS = 5000;

const rollback = "mv /usr/local/bin/shed.prev /usr/local/bin/shed && systemctl restart shed";
const journal = "journalctl -u shed -n 100";

/**
 * RestartScreen covers the dashboard while shed restarts into a new version.
 * Mount it once, high in the tree, so it survives route changes. It renders
 * nothing until useBeginRestart is called.
 */
export function RestartScreen() {
  const restart = useRestart();
  if (!restart) return null;
  return <Waiting key={restart.version} version={restart.version} startedAt={restart.startedAt} />;
}

function Waiting({ version, startedAt }: { version: string; startedAt: number }) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  // Plain fetch on purpose: the shared client may redirect on 401, and every
  // query is expected to fail until the new process is up. Errors and non-2xx
  // responses just mean "not yet".
  useEffect(() => {
    const stop = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const res = await fetch("/api/update", {
          credentials: "same-origin",
          cache: "no-store",
          signal: AbortSignal.any([stop.signal, AbortSignal.timeout(REQUEST_TIMEOUT_MS)]),
        });
        if (res.ok && hasRestarted(await res.json(), version) && !stop.signal.aborted) {
          window.location.reload();
          return;
        }
      } catch {
        // Down or restarting.
      }
      if (!stop.signal.aborted) timer = setTimeout(() => void poll(), POLL_MS);
    };
    void poll();
    return () => {
      stop.abort();
      clearTimeout(timer);
    };
  }, [version]);

  const elapsed = Math.max(0, Math.floor((now - startedAt) / 1000));
  return (
    <RestartOverlay
      version={version}
      elapsed={elapsed}
      stalled={now - startedAt >= timeoutMs}
      onKeepWaiting={() => restartAgain()}
      onReload={() => window.location.reload()}
    />
  );
}

type RestartOverlayProps = {
  version: string;
  /** Seconds since waiting began. */
  elapsed: number;
  /** Waiting has gone on long enough to suggest something is wrong. */
  stalled: boolean;
  onKeepWaiting: () => void;
  onReload: () => void;
  /** Makes the overlay dismissible with Escape; for previews only. */
  onClose?: () => void;
};

/** RestartOverlay is the modal full-screen view of a restart; RestartScreen drives it. */
export function RestartOverlay({
  version,
  elapsed,
  stalled,
  onKeepWaiting,
  onReload,
  onClose,
}: RestartOverlayProps) {
  return (
    <Dialog.Root open modal onOpenChange={(open) => !open && onClose?.()}>
      <Dialog.Portal>
        <Dialog.Backdrop className={styles.backdrop} />
        <Dialog.Popup className={styles.popup}>
          <div className={styles.card}>
            {stalled ? (
              <Callout
                tone="tomato"
                icon={<TriangleAlert size={16} />}
                title={<Dialog.Title render={<span />}>shed hasn't come back</Dialog.Title>}
              >
                <Dialog.Description render={<span />}>
                  Installing {version} is taking longer than expected. Apps keep running.
                </Dialog.Description>
              </Callout>
            ) : (
              <div className={styles.head}>
                <Spinner tone="accent" size={22} />
                <div>
                  <Dialog.Title className={styles.title}>Restarting shed</Dialog.Title>
                  <Dialog.Description className={styles.description}>
                    Installing <span className={styles.mono}>{version}</span>
                  </Dialog.Description>
                </div>
              </div>
            )}

            {stalled ? (
              <div className={styles.hints}>
                <Hint label="Check the log on the server" command={journal} />
                <Hint label="Roll back to the previous version" command={rollback} />
              </div>
            ) : (
              <p className={styles.note}>
                Your apps keep running. The dashboard and every domain are back in a few seconds,
                and this page reloads on its own.
              </p>
            )}

            <div className={styles.foot}>
              <span className={styles.elapsed}>{formatElapsed(elapsed)}</span>
              {stalled && (
                <div className={styles.actions}>
                  <Button onClick={onKeepWaiting}>Keep waiting</Button>
                  <Button variant="primary" onClick={onReload}>
                    Reload
                  </Button>
                </div>
              )}
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function Hint({ label, command }: { label: string; command: string }) {
  return (
    <div>
      <p className={styles.hintLabel}>{label}</p>
      <div className={styles.command}>
        <code>{command}</code>
        <CopyButton value={command} label={`Copy: ${label}`} />
      </div>
    </div>
  );
}

function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, "0")}s`;
}
