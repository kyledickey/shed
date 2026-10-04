import { ChevronRight, CircleCheck, CircleX, LoaderCircle, Search } from "lucide-react";
import { useMemo, useState } from "react";
import type { LogLine, StreamState } from "../../api/events";
import { parseBuildLine, type BuildLine } from "../../lib/logs";
import styles from "./logs.module.css";
import { Highlight, JumpToLatest, StreamIndicator, useFollow } from "./shared";

type Step = {
  title: string;
  lines: { n: number; line: BuildLine }[];
  failed: boolean;
};

function group(lines: LogLine[]): Step[] {
  const steps: Step[] = [];
  lines.forEach((l, i) => {
    const line = parseBuildLine(l.text);
    if (line.kind === "step") {
      steps.push({ title: line.text, lines: [], failed: false });
      return;
    }
    if (steps.length === 0) steps.push({ title: "Preparing", lines: [], failed: false });
    const step = steps.at(-1)!;
    step.lines.push({ n: i + 1, line });
    if (
      (line.kind === "buildkit" && line.state === "error") ||
      (line.kind === "plain" && line.level === "error")
    ) {
      step.failed = true;
    }
  });
  return steps;
}

/**
 * BuildLogView renders build output as collapsible steps, with BuildKit
 * step ids, cache hits, and timings pulled out of the raw text.
 */
export function BuildLogView({
  lines,
  state,
  height = 440,
}: {
  lines: LogLine[];
  state: StreamState;
  height?: number;
}) {
  const steps = useMemo(() => group(lines), [lines]);
  const [query, setQuery] = useState("");
  const [toggled, setToggled] = useState<ReadonlySet<number>>(new Set());
  const { ref, onScroll, following, unseen, jump } = useFollow(lines.length);
  const live = state === "open" || state === "connecting";

  return (
    <div className={styles.viewer}>
      <div className={styles.toolbar}>
        <label className={styles.search}>
          <Search size={14} />
          <input
            placeholder="Search build output"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            spellCheck={false}
          />
        </label>
        <div className={styles.toolbarEnd}>
          <span className={styles.stepCount}>
            {steps.length} steps · {lines.length} lines
          </span>
          <StreamIndicator state={state} />
        </div>
      </div>
      <div className={styles.listWrap} style={{ height }}>
        <div ref={ref} onScroll={onScroll} className={styles.list} role="log">
          {steps.map((step, i) => {
            const last = i === steps.length - 1;
            const running = last && live && !step.failed;
            // Open by default: the running step and failed steps.
            const defaultOpen = running || step.failed || (last && !live);
            const open = toggled.has(i) ? !defaultOpen : defaultOpen;
            const q = query.toLowerCase();
            const rows = q
              ? step.lines.filter(({ line }) => line.text.toLowerCase().includes(q))
              : step.lines;
            if (q && rows.length === 0) return null;
            return (
              <section key={i} className={styles.step} data-open={open || q ? true : undefined}>
                <button
                  type="button"
                  className={styles.stepHead}
                  onClick={() =>
                    setToggled((prev) => {
                      const next = new Set(prev);
                      if (next.has(i)) next.delete(i);
                      else next.add(i);
                      return next;
                    })
                  }
                >
                  <ChevronRight size={14} className={styles.stepChevron} />
                  {step.failed ? (
                    <CircleX size={15} className={styles.stepFailed} />
                  ) : running ? (
                    <LoaderCircle size={15} className={styles.stepRunning} />
                  ) : (
                    <CircleCheck size={15} className={styles.stepDone} />
                  )}
                  <span className={styles.stepTitle}>{step.title}</span>
                  <span className={styles.stepLines}>{step.lines.length}</span>
                </button>
                {(open || q) && (
                  <div className={styles.stepBody}>
                    {rows.map(({ n, line }) => (
                      <BuildRow key={n} n={n} line={line} query={query} />
                    ))}
                  </div>
                )}
              </section>
            );
          })}
        </div>
        {!following && <JumpToLatest unseen={unseen} onClick={jump} />}
      </div>
    </div>
  );
}

function BuildRow({ n, line, query }: { n: number; line: BuildLine; query: string }) {
  if (line.kind === "buildkit") {
    return (
      <div className={styles.buildRow} data-state={line.state ?? undefined}>
        <span className={styles.lineNo}>{n}</span>
        <span className={styles.bkId}>#{line.id}</span>
        <span className={styles.buildText}>
          {line.state === "done" && <span className={styles.bkDone}>done</span>}
          {line.state === "cached" && <span className={styles.bkCached}>cached</span>}
          {line.state === "error" && <span className={styles.bkError}>error</span>}
          <Highlight text={line.text} query={query} />
        </span>
        {line.elapsed && <span className={styles.elapsed}>{line.elapsed}</span>}
      </div>
    );
  }
  if (line.kind === "plain") {
    return (
      <div className={styles.buildRow} data-level={line.level ?? undefined}>
        <span className={styles.lineNo}>{n}</span>
        <span className={styles.buildText}>
          <Highlight text={line.text} query={query} />
        </span>
      </div>
    );
  }
  return null;
}
