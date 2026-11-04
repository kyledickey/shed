import { Button } from "@cloudflare/kumo";
import { ArrowDownIcon } from "@phosphor-icons/react";
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { StreamState } from "../../api/events";
import { Spinner } from "../Badge";
import styles from "./logs.module.css";

/**
 * useFollow keeps a scroll container pinned to the bottom while the user is
 * there, and counts lines that arrive while they have scrolled away.
 */
export function useFollow(count: number) {
  const ref = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  const [seen, setSeen] = useState(count);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el && following) el.scrollTop = el.scrollHeight;
  }, [count, following]);

  useEffect(() => {
    if (following) setSeen(count);
  }, [count, following]);

  const onScroll = () => {
    const el = ref.current;
    if (!el) return;
    setFollowing(el.scrollHeight - el.scrollTop - el.clientHeight < 32);
  };

  const jump = () => {
    const el = ref.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    setFollowing(true);
  };

  return { ref, onScroll, following, unseen: Math.max(0, count - seen), jump };
}

/** JumpToLatest floats over a scrolled-up log and returns to the newest line. */
export function JumpToLatest({ unseen, onClick }: { unseen: number; onClick: () => void }) {
  return (
    <div className={styles.jump}>
      <Button size="sm" icon={<ArrowDownIcon size={14} />} onClick={onClick}>
        {unseen > 0 ? `${unseen} new line${unseen === 1 ? "" : "s"}` : "Jump to latest"}
      </Button>
    </div>
  );
}

const streamLabels: Record<StreamState, string> = {
  connecting: "Connecting",
  open: "Live",
  reconnecting: "Reconnecting",
  ended: "Ended",
  closed: "Disconnected",
};

/**
 * StreamIndicator says whether a log stream is live. A spinner means lines
 * are arriving; a lost connection reads in the danger color.
 */
export function StreamIndicator({ state, paused }: { state: StreamState; paused?: boolean }) {
  const live = (state === "open" || state === "connecting" || state === "reconnecting") && !paused;
  return (
    <span className={styles.stream} data-closed={(state === "closed" && !paused) || undefined}>
      {live && <Spinner tone={state === "open" ? "grass" : "neutral"} size={10} />}
      {paused ? "Paused" : streamLabels[state]}
    </span>
  );
}

/** Highlight wraps case-insensitive matches of query in <mark>. */
export function Highlight({ text, query }: { text: string; query: string }): ReactNode {
  if (!query) return text;
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  const out: ReactNode[] = [];
  let from = 0;
  for (let i = lower.indexOf(q); i !== -1; i = lower.indexOf(q, from)) {
    out.push(text.slice(from, i), <mark key={i}>{text.slice(i, i + q.length)}</mark>);
    from = i + q.length;
  }
  out.push(text.slice(from));
  return out;
}

const timeFmt = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

export function LogTime({ time }: { time: Date | null }) {
  if (!time) return <span className={styles.time} />;
  const ms = String(time.getMilliseconds()).padStart(3, "0");
  return (
    <time className={styles.time} dateTime={time.toISOString()} title={time.toLocaleString()}>
      {timeFmt.format(time)}
      <span className={styles.ms}>.{ms}</span>
    </time>
  );
}
