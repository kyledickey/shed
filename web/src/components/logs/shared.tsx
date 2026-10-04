import { ArrowDown } from "lucide-react";
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

export function JumpToLatest({ unseen, onClick }: { unseen: number; onClick: () => void }) {
  return (
    <button type="button" className={styles.jump} onClick={onClick}>
      <ArrowDown size={13} />
      {unseen > 0 ? `${unseen} new line${unseen === 1 ? "" : "s"}` : "Jump to latest"}
    </button>
  );
}

const streamLabels: Record<StreamState, string> = {
  connecting: "Connecting",
  open: "Live",
  reconnecting: "Reconnecting",
  ended: "Ended",
  closed: "Disconnected",
};

export function StreamIndicator({ state, paused }: { state: StreamState; paused?: boolean }) {
  const tone = paused
    ? "sunflower"
    : state === "open"
      ? "grass"
      : state === "closed"
        ? "tomato"
        : "neutral";
  return (
    <span className={styles.stream} data-tone={tone}>
      {(state === "open" || state === "connecting" || state === "reconnecting") && !paused && (
        <Spinner tone={tone} size={9} />
      )}
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
