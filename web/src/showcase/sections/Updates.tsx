import { useEffect, useState } from "react";
import type { UpdateStatus } from "../../api/types";
import { Button } from "../../components/Button";
import { UpdatesCardView } from "../../features/settings/UpdatesCard";
import { RestartOverlay } from "../../features/update/RestartScreen";
import { Section } from "../Section";
import s from "../showcase.module.css";

const hoursAgo = (h: number) => new Date(Date.now() - h * 3_600_000).toISOString();

const base: UpdateStatus = {
  current: "v1.3.2",
  latest: {
    version: "v1.4.0",
    url: "https://github.com/example/shed/releases/tag/v1.4.0",
    notes:
      "## Highlights\n\n- Self-update from the dashboard\n- Faster build log streaming\n\n## Fixes\n\n- Backups no longer stall on large volumes",
    publishedAt: hoursAgo(30),
  },
  available: true,
  checkedAt: hoursAgo(2),
  state: "idle",
  staged: "",
  error: "",
  autoDownload: false,
  unsupported: "",
};

const states: { label: string; status: UpdateStatus }[] = [
  { label: "Available", status: base },
  { label: "Downloading", status: { ...base, state: "downloading" } },
  { label: "Ready to install", status: { ...base, staged: "v1.4.0" } },
  {
    label: "Up to date",
    status: { ...base, current: "v1.4.0", available: false, autoDownload: true },
  },
  {
    label: "Check failed",
    status: {
      ...base,
      available: false,
      error: "check releases: GET api.github.com: connection refused",
    },
  },
  {
    label: "Development build",
    status: {
      ...base,
      current: "dev",
      latest: null,
      available: false,
      checkedAt: null,
      unsupported: "This is a development build; install a release to enable updates.",
    },
  },
];

const noop = () => {};

type Preview = "restarting" | "stalled" | null;

/** Updates previews the Settings update card in each state and the restart screen. */
export function Updates() {
  const [preview, setPreview] = useState<Preview>(null);
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    if (!preview) return;
    setElapsed(preview === "stalled" ? 181 : 0);
    const id = setInterval(() => setElapsed((e) => e + 1), 1000);
    return () => clearInterval(id);
  }, [preview]);

  return (
    <>
      {states.map(({ label, status }) => (
        <Section key={label} title={label}>
          <UpdatesCardView
            status={status}
            checking={false}
            downloading={false}
            installing={false}
            onCheck={noop}
            onDownload={noop}
            onAutoDownload={noop}
            onInstall={noop}
            onCloseInstall={noop}
          />
        </Section>
      ))}
      <Section
        title="Restart screen"
        description="Covers the dashboard while shed restarts into a new version. Press Esc to close a preview."
      >
        <div className={s.row}>
          <Button onClick={() => setPreview("restarting")}>Restarting</Button>
          <Button onClick={() => setPreview("stalled")}>Not coming back</Button>
        </div>
      </Section>
      {preview && (
        <RestartOverlay
          version="v1.4.0"
          elapsed={elapsed}
          stalled={preview === "stalled"}
          onKeepWaiting={() => setElapsed(0)}
          onReload={() => setPreview(null)}
          onClose={() => setPreview(null)}
        />
      )}
    </>
  );
}
