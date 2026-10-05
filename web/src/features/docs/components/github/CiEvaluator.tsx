import { useState } from "react";
import { Badge } from "../../../../components/Badge";
import { Select } from "../../../../components/Form";
import { Demo } from "../../kit";
import { ciActions, ciState, type CheckRun, type CombinedStatus } from "../../lib/ci";
import styles from "./github.module.css";

const runOptions = [
  { value: "queued", label: "queued" },
  { value: "in_progress", label: "in progress" },
  { value: "success", label: "completed: success" },
  { value: "neutral", label: "completed: neutral" },
  { value: "skipped", label: "completed: skipped" },
  { value: "failure", label: "completed: failure" },
  { value: "cancelled", label: "completed: cancelled" },
  { value: "timed_out", label: "completed: timed out" },
];

const statusOptions = [
  { value: "none", label: "no commit statuses" },
  { value: "success", label: "combined: success" },
  { value: "pending", label: "combined: pending" },
  { value: "failure", label: "combined: failure" },
  { value: "error", label: "combined: error" },
];

function toRun(value: string): CheckRun {
  if (value === "queued" || value === "in_progress")
    return { status: value, conclusion: "success" };
  return { status: "completed", conclusion: value as CheckRun["conclusion"] };
}

function toCombined(value: string): CombinedStatus {
  return value === "none"
    ? { total: 0, state: "pending" }
    : { total: 1, state: value as CombinedStatus["state"] };
}

const tones = { success: "grass", failure: "tomato", pending: "neutral", none: "neutral" } as const;
const labels = {
  success: "Success",
  failure: "Failure",
  pending: "Pending",
  none: "No results",
} as const;

/** CiEvaluator lets the reader set check results and see what shed's CI wait decides. */
export function CiEvaluator() {
  const [runs, setRuns] = useState(["success", "in_progress"]);
  const [status, setStatus] = useState("none");
  const state = ciState(runs.map(toRun), toCombined(status));

  return (
    <Demo title="Try the CI wait">
      <div className={styles.evaluator}>
        <div className={styles.evaluatorInputs}>
          {runs.map((r, i) => (
            <Select
              key={i}
              value={r}
              options={runOptions}
              onChange={(v) => setRuns(runs.map((old, j) => (j === i ? v : old)))}
            />
          ))}
          <Select value={status} options={statusOptions} onChange={setStatus} />
        </div>
        <div className={styles.evaluatorResult}>
          <Badge tone={tones[state]}>{labels[state]}</Badge>
          <span>{ciActions[state]}</span>
        </div>
      </div>
    </Demo>
  );
}
