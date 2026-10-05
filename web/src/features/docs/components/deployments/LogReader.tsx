import { useState } from "react";
import { Badge } from "../../../../components/Badge";
import { Textarea } from "../../../../components/Form";
import {
  headingText,
  pipelineSteps,
  readProgress,
  stepOfHeading,
  stepStates,
  type StepState,
} from "../../lib/buildLog";
import { Demo } from "../../kit";
import styles from "./deployments.module.css";

const sample = `==> Building acme/web@a1b2c3d
==> Cloning
==> Building with Dockerfile
#8 DONE 14.2s
==> Starting container
Name: shed-p4n8-r7c1
Started container 3f9a1c2b7d10
==> Waiting for port 3000 to become healthy
Probing TCP connect to 172.18.0.4:3000 (timeout 2m0s)
Not ready yet: dial tcp 172.18.0.4:3000: connect: connection refused
==> Deployment failed: health check timed out after 2m0s`;

const labels: Record<StepState, string> = {
  done: "Done",
  current: "Running",
  failed: "Stopped here",
  unused: "Not used",
  pending: "Not reached",
};

/** LogReader maps a pasted build log to the pipeline stages it reached. */
export function LogReader() {
  const [log, setLog] = useState(sample);
  const headings = log
    .split("\n")
    .map(headingText)
    .filter((h): h is string => h !== null);
  const progress = readProgress(headings);
  const outcome = progress.ended?.status ?? (headings.length > 0 ? "deploying" : "queued");
  const states = stepStates(progress, outcome);

  return (
    <Demo title="Place a build log on the pipeline">
      <div className={styles.body}>
        <Textarea
          mono
          rows={8}
          spellCheck={false}
          aria-label="Build log"
          value={log}
          onChange={(e) => setLog(e.target.value)}
        />
        <ol className={styles.steps}>
          {pipelineSteps.map((s) => {
            const state = states[s.id];
            return (
              <li
                key={s.id}
                className={styles.step}
                data-state={state}
                data-tone={state === "failed" ? "tomato" : undefined}
              >
                <span className={styles.stepTitle}>{s.title}</span>
                <span className={styles.stepState}>
                  {state === "failed" && outcome === "skipped"
                    ? "CI failed"
                    : state === "failed" && outcome === "canceled"
                      ? "Canceled here"
                      : labels[state]}
                </span>
              </li>
            );
          })}
        </ol>
        {progress.ended && (
          <p className={styles.note}>
            Ended <code>{progress.ended.status}</code>: {progress.ended.reason}
          </p>
        )}
        {progress.truncated && (
          <p className={styles.note}>
            The log hit its size cap, so the last stages may be missing.
          </p>
        )}
        <ol className={styles.headings}>
          {headings.map((h, i) => {
            const step = pipelineSteps.find((s) => s.id === stepOfHeading(h));
            return (
              <li key={i} className={styles.heading}>
                <code>==&gt; {h}</code>
                {step && <Badge size="sm">{step.title}</Badge>}
              </li>
            );
          })}
        </ol>
      </div>
    </Demo>
  );
}
