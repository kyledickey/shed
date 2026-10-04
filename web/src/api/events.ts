import { useEffect, useRef, useState } from "react";

export type StreamState = "connecting" | "open" | "reconnecting" | "ended" | "closed";

type Handlers = {
  open?: () => void;
  log?: (line: string) => void;
  status?: (status: string) => void;
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
    es.addEventListener("status", (e) => handlersRef.current.status?.(parseStatus(e.data)));
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

function parseStatus(data: string): string {
  try {
    const parsed: unknown = JSON.parse(data);
    if (typeof parsed === "string") return parsed;
    if (parsed && typeof parsed === "object" && "status" in parsed) {
      return String(parsed.status);
    }
  } catch {
    // Plain-text status.
  }
  return data.trim();
}

export type LogLine = { id: number; text: string };

const MAX_LINES = 5000;
// oxlint-disable-next-line no-control-regex
const ANSI = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;

/**
 * Collects `log` events into a capped list of lines, batching updates per
 * animation frame. The server replays history on (re)connect, so lines reset
 * whenever the stream opens.
 */
export function useLogStream(url: string | null, onStatus?: (status: string) => void) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const pending = useRef<LogLine[]>([]);
  const frame = useRef(0);
  const nextId = useRef(0);

  useEffect(() => () => cancelAnimationFrame(frame.current), []);

  const flush = () => {
    frame.current = 0;
    const batch = pending.current;
    pending.current = [];
    setLines((prev) => {
      const next = prev.concat(batch);
      return next.length > MAX_LINES ? next.slice(-MAX_LINES) : next;
    });
  };

  const clear = () => {
    pending.current = [];
    setLines([]);
  };

  const state = useEventSource(url, {
    open: clear,
    log: (raw) => {
      pending.current.push({
        id: nextId.current++,
        text: raw.replace(ANSI, "").replace(/\r$/, ""),
      });
      if (!frame.current) frame.current = requestAnimationFrame(flush);
    },
    status: onStatus,
  });

  return { lines, state, clear };
}
