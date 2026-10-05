/** StepId names a stage of the deploy pipeline. */
export type StepId = "ci" | "build" | "start" | "health" | "switch";

/** pipelineSteps lists the stages in the order a deployment runs them. */
export const pipelineSteps: { id: StepId; title: string; status: string }[] = [
  { id: "ci", title: "Wait for CI", status: "waiting" },
  { id: "build", title: "Build or pull", status: "building" },
  { id: "start", title: "Start container", status: "deploying" },
  { id: "health", title: "Health check", status: "deploying" },
  { id: "switch", title: "Switch traffic", status: "deploying" },
];

/** What a build log says about how far its deployment got. */
export type LogProgress = {
  /** The last stage that printed a heading, or null before the first. */
  reached: StepId | null;
  /** The deployment waited for CI. */
  usedCi: boolean;
  /** The closing "==> Deployment <status>: <reason>" line, if the pipeline ended in one. */
  ended: { status: string; reason: string } | null;
  /** The log hit its size cap and later output was dropped. */
  truncated: boolean;
};

const stepPatterns: [RegExp, StepId][] = [
  [/^Waiting for CI on /, "ci"],
  [/^CI passed/, "ci"],
  [/^(Reusing image|Pulling|Building|Cloning|Detected port) ?/, "build"],
  [/^(Stopping previous deployment|Starting container)/, "start"],
  [/^(Waiting for .+ to become healthy|Watching the container start)$/, "health"],
  [/^Switching traffic/, "switch"],
];

/** headingText returns the text of a pipeline heading line, or null for any other line. */
export function headingText(line: string): string | null {
  return line.startsWith("==> ") ? line.slice(4) : null;
}

/** stepOfHeading maps heading text to the pipeline stage that prints it. */
export function stepOfHeading(text: string): StepId | null {
  return stepPatterns.find(([re]) => re.test(text))?.[1] ?? null;
}

/** readProgress summarizes the headings of a build log, in order. */
export function readProgress(headings: string[]): LogProgress {
  const progress: LogProgress = { reached: null, usedCi: false, ended: null, truncated: false };
  let at = -1;
  for (const h of headings) {
    const ended = /^Deployment (\w+): (.*)$/.exec(h);
    if (ended) {
      progress.ended = { status: ended[1]!, reason: ended[2]! };
    } else if (h.startsWith("Log truncated")) {
      progress.truncated = true;
    } else {
      const step = stepOfHeading(h);
      if (step === "ci") progress.usedCi = true;
      const i = pipelineSteps.findIndex((s) => s.id === step);
      if (i > at) at = i;
    }
  }
  progress.reached = pipelineSteps[at]?.id ?? null;
  return progress;
}

/** StepState is how one stage looks for a given deployment. */
export type StepState = "done" | "current" | "failed" | "unused" | "pending";

/**
 * stepStates places a deployment on the pipeline. `outcome` is the
 * deployment's status. A deployment that ended without going live broke in
 * the last stage that printed a heading.
 */
export function stepStates(progress: LogProgress, outcome: string): Record<StepId, StepState> {
  const at = pipelineSteps.findIndex((s) => s.id === progress.reached);
  const live = outcome === "active" || outcome === "removed";
  const broke = outcome === "failed" || outcome === "skipped" || outcome === "canceled";
  const result = {} as Record<StepId, StepState>;
  pipelineSteps.forEach((s, i) => {
    if (s.id === "ci" && !progress.usedCi && (live || i < at)) result[s.id] = "unused";
    else if (live || i < at) result[s.id] = "done";
    else if (i === at) result[s.id] = broke ? "failed" : "current";
    else result[s.id] = "pending";
  });
  return result;
}
