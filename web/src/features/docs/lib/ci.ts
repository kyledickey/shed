/** One GitHub check run, reduced to what shed reads. */
export type CheckRun = {
  status: "queued" | "in_progress" | "completed";
  /** Only meaningful once the run is completed. */
  conclusion:
    | "success"
    | "neutral"
    | "skipped"
    | "failure"
    | "cancelled"
    | "timed_out"
    | "action_required";
};

/** The combined commit status GitHub reports for a SHA. */
export type CombinedStatus = { total: number; state: "success" | "pending" | "failure" | "error" };

/** The aggregate CI state, as shed folds check runs and commit statuses. */
export type CIState = "success" | "pending" | "failure" | "none";

/** What the pipeline does with each CI state. */
export const ciActions: Record<CIState, string> = {
  success: "Continues to the build.",
  pending: "Keeps polling every 10 seconds.",
  none: "Keeps polling. No checks is not approval; they may not exist yet.",
  failure: "Ends the deployment as skipped.",
};

/** ciState folds check runs and commit statuses: a failure outranks pending, and no results at all is "none". */
export function ciState(runs: CheckRun[], combined: CombinedStatus): CIState {
  let seen = runs.length > 0;
  let pending = false;
  let failed = false;
  for (const run of runs) {
    if (run.status !== "completed") {
      pending = true;
    } else if (!["success", "neutral", "skipped"].includes(run.conclusion)) {
      failed = true;
    }
  }
  if (combined.total > 0) {
    seen = true;
    if (combined.state === "pending") pending = true;
    else if (combined.state !== "success") failed = true;
  }
  if (failed) return "failure";
  if (pending) return "pending";
  return seen ? "success" : "none";
}
