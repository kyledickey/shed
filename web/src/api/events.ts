import { useEffect, useRef, useState } from "react";
import { PendingLines } from "./pending";
import type { DeploymentStatus } from "./types";

export type StreamState = "connecting" | "open" | "reconnecting" | "ended" | "closed";

type Handlers = {
  open?: () => void;
  log?: (line: string) => void;
  status?: (status: DeploymentStatus) => void;
  end?: () => void;
};

/**
 * Subscribes to a shed SSE endpoint. Handlers may change between renders
 * without reconnecting; changing the url reconnects.
 */
export function useEventSource(url: string | null, handlers: Handlers): StreamState {
  const handlersRef = useRef(handlers);
  const [state, setState] = useState<{ url: string | null; value: StreamState }>({
    url,
    value: "connecting",
  });

  useEffect(() => {
    handlersRef.current = handlers;
  });

  useEffect(() => {
    if (!url) return;
    const es = new EventSource(url);
    const set = (value: StreamState) => setState({ url, value });

    es.addEventListener("open", () => {
      set("open");
      handlersRef.current.open?.();
    });
    es.addEventListener("log", (e) => handlersRef.current.log?.(e.data));
    es.addEventListener("status", (e) => {
      const { status } = JSON.parse(e.data) as { status: DeploymentStatus };
      handlersRef.current.status?.(status);
    });
    es.addEventListener("end", () => {
      es.close();
      set("ended");
      handlersRef.current.end?.();
    });
    es.addEventListener("error", () => {
      set(es.readyState === EventSource.CLOSED ? "closed" : "reconnecting");
    });
    return () => es.close();
  }, [url]);

  return state.url === url ? state.value : "connecting";
}

export type LogLine = { id: number; text: string };

const MAX_LINES = 5000;
/** Characters of text buffered between renders; the server splits lines at about 64 KiB. */
const MAX_PENDING_CHARS = 4 << 20;
/** How often lines are flushed while the tab is hidden and animation frames pause. */
const HIDDEN_FLUSH_MS = 1000;
// oxlint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;

/**
 * Collects `log` events into a capped list of lines, batching updates per
 * animation frame, or on a timer while the tab is hidden. Lines waiting for
 * a render are bounded too; if older ones are dropped, a marker line says how
 * many. The server replays history on (re)connect, so lines reset whenever
 * the stream opens.
 */
export function useLogStream(url: string | null, onStatus?: (status: DeploymentStatus) => void) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const pending = useRef(new PendingLines<LogLine>(MAX_LINES, MAX_PENDING_CHARS));
  const cancelFlush = useRef<(() => void) | null>(null);
  const nextId = useRef(0);

  useEffect(() => () => cancelFlush.current?.(), []);

  const flush = () => {
    cancelFlush.current = null;
    const { lines: batch, dropped } = pending.current.take();
    if (dropped > 0) {
      batch.unshift({
        id: nextId.current++,
        text: `… ${dropped} earlier ${dropped === 1 ? "line" : "lines"} dropped`,
      });
    }
    setLines((prev) => {
      const next = prev.concat(batch);
      return next.length > MAX_LINES ? next.slice(-MAX_LINES) : next;
    });
  };

  const scheduleFlush = () => {
    if (cancelFlush.current) return;
    if (document.hidden) {
      const timer = setTimeout(flush, HIDDEN_FLUSH_MS);
      cancelFlush.current = () => clearTimeout(timer);
    } else {
      const frame = requestAnimationFrame(flush);
      cancelFlush.current = () => cancelAnimationFrame(frame);
    }
  };

  const clear = () => {
    pending.current.clear();
    setLines([]);
  };

  const state = useEventSource(url, {
    open: clear,
    log: (raw) => {
      pending.current.push({
        id: nextId.current++,
        text: raw.replace(ANSI, "").replace(/\r$/, ""),
      });
      scheduleFlush();
    },
    status: onStatus,
  });

  return { lines, state, clear };
}
