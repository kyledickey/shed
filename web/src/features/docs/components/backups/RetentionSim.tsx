import { useState } from "react";
import { Input, Switch } from "../../../../components/Form";
import { Demo } from "../../kit";
import { simulateRetention, timelineLength, type TimelineRow } from "../../lib/retention";
import styles from "./backups.module.css";

type State = { keepLocal: number; keepRemote: number; upload: boolean };

const defaults: State = { keepLocal: 7, keepRemote: 30, upload: true };

/** validate mirrors the policy rules shed enforces when saving. */
function validate(s: State): string | null {
  if (s.keepLocal === 0 && !s.upload)
    return "shed rejects this: keep at least one backup locally, or upload them.";
  if (s.upload && s.keepRemote === 0)
    return "shed rejects this: keep at least one backup in S3 when uploading.";
  return null;
}

function copy(had: boolean, kept: boolean): { text: string; className: string } {
  if (!had) return { text: "none", className: styles.muted! };
  return kept
    ? { text: "kept", className: styles.keep! }
    : { text: "removed", className: styles.drop! };
}

function Row({ r }: { r: TimelineRow }) {
  const local = copy(r.hadLocal, r.local);
  const remote = copy(r.hadRemote, r.remote);
  return (
    <>
      <span className={r.scheduled ? styles.muted : styles.manual}>
        {r.scheduled ? (r.daysAgo === 0 ? "latest" : `${r.daysAgo}d ago`) : "manual"}
      </span>
      <span className={local.className}>{local.text}</span>
      <span className={remote.className}>{remote.text}</span>
      {r.deleted ? <span className={styles.gone}>deleted</span> : <span>stays</span>}
    </>
  );
}

/** RetentionSim runs shed's retention rules over a made-up timeline of backups. */
export function RetentionSim() {
  const [state, setState] = useState<State>(defaults);
  const error = validate(state);
  const rows = error ? [] : simulateRetention(state);

  const number = (key: "keepLocal" | "keepRemote", label: string) => (
    <label className={styles.control}>
      {label}
      <Input
        type="number"
        min={0}
        max={60}
        value={state[key]}
        onChange={(e) =>
          setState({ ...state, [key]: Math.max(0, Math.floor(Number(e.target.value) || 0)) })
        }
      />
    </label>
  );

  return (
    <Demo title="Retention simulator">
      <div className={styles.stack}>
        <div className={styles.controls}>
          {number("keepLocal", "keepLocal")}
          {number("keepRemote", "keepRemote")}
          <Switch
            label="upload"
            checked={state.upload}
            onChange={(upload) => setState({ ...state, upload })}
          />
        </div>
        <p className={styles.summary}>
          {timelineLength} daily scheduled backups and one manual backup, each starting with the
          copies its policy would have made. The table shows what is left right after the newest
          scheduled backup finishes and retention runs.
        </p>
        {error ? (
          <p className={styles.gone}>{error}</p>
        ) : (
          <div className={styles.timeline}>
            {["Backup", "Local file", "S3 object", "Backup row"].map((h) => (
              <span key={h} className={styles.timelineHead}>
                {h}
              </span>
            ))}
            {rows.map((r) => (
              <Row key={r.id} r={r} />
            ))}
          </div>
        )}
      </div>
    </Demo>
  );
}
