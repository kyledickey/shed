import { InputGroup, LayerCard } from "@cloudflare/kumo";
import { CaretRightIcon, CheckIcon, MagnifyingGlassIcon, XIcon } from "@phosphor-icons/react";
import { useMemo, useState } from "react";
import type { LogLine, StreamState } from "../../api/events";
import { parseBuildLine, type BuildLine } from "../../lib/logs";
import { Spinner } from "../Badge";
import { CountChip } from "../Pill";
import styles from "./logs.module.css";
import { Highlight, JumpToLatest, StreamIndicator, useFollow } from "./shared";

type Step = {
  title: string;
  lines: { n: number; line: BuildLine }[];
  failed: boolean;
};

const EXPAND_ALL_MAX = 400;

function group(lines: LogLine[]): Step[] {
  const steps: Step[] = [];
  lines.forEach((l, i) => {
    const line = parseBuildLine(l.text);
    if (line.kind === "step") {
      // The pipeline closes a failed run with "==> Deployment failed: …".
      steps.push({
        title: line.text,
        lines: [],
        failed: /^Deployment (failed|canceled)/.test(line.text),
      });
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
  // A finished, short log reads best fully expanded.
  const expandAll = !live && lines.length <= EXPAND_ALL_MAX;

  return (
    <LayerCard className={styles.viewer}>
      <LayerCard.Secondary className={styles.bar}>
        <div className={styles.toolbar}>
          <InputGroup size="sm" className={styles.search}>
            <InputGroup.Addon>
              <MagnifyingGlassIcon size={14} />
            </InputGroup.Addon>
            <InputGroup.Input
              aria-label="Search build output"
              placeholder="Search build output"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              spellCheck={false}
            />
          </InputGroup>
          <div className={styles.toolbarEnd}>
            <span className={styles.stepCount}>
              {steps.length} steps · {lines.length} lines
            </span>
            <StreamIndicator state={state} />
          </div>
        </div>
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.panel}>
        <div className={styles.listWrap} style={{ height }}>
          <div ref={ref} onScroll={onScroll} className={styles.list} role="log">
            {steps.map((step, i) => {
              const last = i === steps.length - 1;
              const running = last && live && !step.failed;
              // Open by default: the running step and failed steps.
              const defaultOpen = expandAll || running || step.failed || (last && !live);
              const empty = step.lines.length === 0;
              const open = toggled.has(i) ? !defaultOpen : defaultOpen;
              const q = query.toLowerCase();
              const rows = q
                ? step.lines.filter(({ line }) => line.text.toLowerCase().includes(q))
                : step.lines;
              if (q && rows.length === 0) return null;
              return (
                <section
                  key={i}
                  className={styles.step}
                  data-open={(!empty && (open || q)) || undefined}
                >
                  <button
                    type="button"
                    className={styles.stepHead}
                    disabled={empty}
                    onClick={() =>
                      setToggled((prev) => {
                        const next = new Set(prev);
                        if (next.has(i)) next.delete(i);
                        else next.add(i);
                        return next;
                      })
                    }
                  >
                    <CaretRightIcon size={12} className={styles.stepChevron} />
                    <span className={styles.stepGlyph}>
                      {step.failed ? (
                        <XIcon size={14} weight="bold" className={styles.stepFailed} />
                      ) : running ? (
                        <Spinner size={11} />
                      ) : (
                        <CheckIcon size={14} weight="bold" />
                      )}
                    </span>
                    <span className={styles.stepTitle}>{step.title}</span>
                    {!empty && <CountChip>{step.lines.length}</CountChip>}
                  </button>
                  {!empty && (open || q) && (
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
      </LayerCard.Primary>
    </LayerCard>
  );
}

function BuildRow({ n, line, query }: { n: number; line: BuildLine; query: string }) {
  if (line.kind === "buildkit") {
    return (
      <div className={styles.buildRow} data-state={line.state ?? undefined}>
        <span className={styles.lineNo}>{n}</span>
        <span className={styles.bkId}>#{line.id}</span>
        <span className={styles.buildText}>
          {line.state === "done" && <span className={styles.bkTag}>done</span>}
          {line.state === "cached" && <span className={styles.bkTag}>cached</span>}
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
