// Parsers that turn raw container and build log lines into structured values
// for the dashboard. Input lines are already stripped of ANSI codes and "\r".

/** LogLevel is a normalized severity. */
export type LogLevel = "debug" | "info" | "warn" | "error";

/** HttpRequest is an access-log entry detected in a log line. */
export type HttpRequest = {
  method: string;
  path: string;
  status: number;
  duration: string | null;
};

/** RuntimeLog is one parsed container log line. */
export type RuntimeLog = {
  /** Docker timestamp, else a timestamp found in the line itself. */
  time: Date | null;
  level: LogLevel | null;
  /** The human message, without any consumed level or timestamp. */
  message: string;
  /** Remaining structured key/values in source order. */
  fields: [string, string][];
  http: HttpRequest | null;
};

/** BuildLine is one parsed line of build output. */
export type BuildLine =
  | { kind: "step"; text: string }
  | {
      kind: "buildkit";
      id: number;
      text: string;
      state: "done" | "cached" | "error" | null;
      elapsed: string | null;
    }
  | { kind: "plain"; text: string; level: LogLevel | null };

const LEVELS: Record<string, LogLevel> = {
  trace: "debug",
  debug: "debug",
  info: "info",
  notice: "info",
  log: "info",
  warn: "warn",
  warning: "warn",
  error: "error",
  err: "error",
  fatal: "error",
  panic: "error",
  critical: "error",
  crit: "error",
  alert: "error",
  emerg: "error",
};

const LEVEL_KEYS = ["level", "lvl", "severity", "log.level"];
const MESSAGE_KEYS = ["msg", "message", "event"];
const TIME_KEYS = ["time", "ts", "timestamp", "@timestamp"];

const DOCKER_TIME = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))(?:\s|$)/;
const TEXT_TIME =
  /^\[?(\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?)\]?[\s:-]*/;
const LEVEL_TOKEN =
  /^\[?\s*(trace|debug|info|notice|warn(?:ing)?|err(?:or)?|fatal|critical|crit|panic)\s*\]?(?=[:\s[]|$)[:\s]*/i;
const LEVEL_FIELD = /^level[:=]\s*([a-z]+)\s*/i;
const NPM_LEVEL = /^npm (ERR|WARN)\b/;
const ERROR_START = /^(?:panic:|Traceback\b|[\w.$]*Exception\b)/;

const METHODS = "GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS";
const DURATION = String.raw`\d+(?:\.\d+)?\s?(?:ns|µs|us|ms|s)\b`;
const COMMON_LOG = new RegExp(String.raw`"(${METHODS}) (\S+)(?: HTTP/[\d.]+)?"\s+(\d{3})\b`);
const SIMPLE_LOG = new RegExp(
  String.raw`(?:^|\s)(${METHODS}) (/\S*)(?:\s+-)?\s+(\d{3})\b(?:\s+-)?(?:\s+(${DURATION}))?`,
);

/** normalizeLevel maps a level name or pino/bunyan number to a LogLevel. */
function normalizeLevel(v: unknown): LogLevel | null {
  if (typeof v === "number") {
    if (v <= 20) return v >= 10 ? "debug" : null;
    if (v <= 30) return "info";
    if (v <= 40) return "warn";
    return v <= 60 ? "error" : null;
  }
  return typeof v === "string" ? (LEVELS[v.trim().toLowerCase()] ?? null) : null;
}

function parseTime(v: unknown): Date | null {
  let d: Date;
  if (typeof v === "number") d = new Date(v < 1e11 ? v * 1000 : v);
  else if (typeof v === "string") {
    let s = v.trim().replace(" ", "T").replace(",", ".").replaceAll("/", "-");
    if (!/(Z|[+-]\d{2}:?\d{2})$/.test(s)) s += "Z";
    d = new Date(s);
  } else return null;
  return Number.isNaN(d.getTime()) ? null : d;
}

function detectLevel(text: string): { level: LogLevel | null; rest: string } {
  const npm = NPM_LEVEL.exec(text);
  if (npm) return { level: npm[1] === "ERR" ? "error" : "warn", rest: text };

  const field = LEVEL_FIELD.exec(text);
  const fieldLevel = field && normalizeLevel(field[1]);
  if (field && fieldLevel) return { level: fieldLevel, rest: text.slice(field[0].length) };

  const m = LEVEL_TOKEN.exec(text);
  if (m) {
    const token = m[1] ?? "";
    const level = normalizeLevel(token);
    const bracketed = m[0].startsWith("[");
    if (bracketed || token === token.toUpperCase()) return { level, rest: text.slice(m[0].length) };
    // "Error: x" or "warning: x" keeps its text; a bare lowercase word isn't a level.
    if (m[0].includes(":")) return { level, rest: text };
  }
  return { level: ERROR_START.test(text) ? "error" : null, rest: text };
}

function detectHttpText(text: string): HttpRequest | null {
  const common = COMMON_LOG.exec(text);
  const m = common ?? SIMPLE_LOG.exec(text);
  if (!m) return null;
  const status = Number(m[3]);
  if (status < 100 || status > 599) return null;
  const duration = common ? null : (m[4]?.replace(/\s/g, "") ?? null);
  return { method: m[1] ?? "", path: m[2] ?? "", status, duration };
}

const HTTP_KEYS = new Set([
  "method",
  "path",
  "url",
  "uri",
  "status",
  "status_code",
  "duration",
  "latency",
  "elapsed",
]);

function detectHttpFields(entries: [string, unknown][]): HttpRequest | null {
  const get = (...keys: string[]) => {
    for (const k of keys) {
      const v = entries.find(([key]) => key === k)?.[1];
      if (typeof v === "string" || typeof v === "number") return v;
    }
    return null;
  };
  const method = get("method");
  const path = get("path", "url", "uri");
  const status = Number(get("status", "status_code"));
  if (typeof method !== "string" || typeof path !== "string") return null;
  if (!Number.isInteger(status) || status < 100 || status > 599) return null;
  const d = get("duration", "latency", "elapsed");
  const duration = typeof d === "number" ? `${d}ms` : (d?.replace(/\s/g, "") ?? null);
  return { method: method.toUpperCase(), path, status, duration };
}

function levelFromStatus(status: number): LogLevel {
  return status >= 500 ? "error" : status >= 400 ? "warn" : "info";
}

function stringify(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v);
}

/** fromEntries builds a RuntimeLog from structured key/value pairs. */
function fromEntries(
  entries: [string, unknown][],
): Omit<RuntimeLog, "time"> & { time: Date | null } {
  const used = new Set<number>();
  const take = <T>(keys: string[], convert: (v: unknown) => T | null): T | null => {
    for (const k of keys) {
      const i = entries.findIndex(([key]) => key === k);
      const value = i < 0 ? null : convert(entries[i]?.[1]);
      if (value !== null) {
        used.add(i);
        return value;
      }
    }
    return null;
  };

  const message = take(MESSAGE_KEYS, (v) =>
    typeof v === "string" || typeof v === "number" ? String(v) : null,
  );
  let level = take(LEVEL_KEYS, normalizeLevel);
  const time = take(TIME_KEYS, parseTime);
  const fromFields = detectHttpFields(entries);
  const http = fromFields ?? detectHttpText(message ?? "");
  if (!level && http) level = levelFromStatus(http.status);

  const fields = entries
    .filter(([k], i) => !used.has(i) && !(fromFields && HTTP_KEYS.has(k)))
    .map(([k, v]): [string, string] => [k, stringify(v)]);
  return { time, level, message: message ?? "", fields, http };
}

function parseJson(body: string): RuntimeLog | null {
  if (!body.startsWith("{")) return null;
  let obj: unknown;
  try {
    obj = JSON.parse(body);
  } catch {
    return null;
  }
  if (typeof obj !== "object" || obj === null || Array.isArray(obj)) return null;
  const entries = Object.entries(obj);
  // ECS-style nested {"log":{"level":"info"}}.
  const nested = (obj as { log?: { level?: unknown } }).log?.level;
  if (nested !== undefined && !entries.some(([k]) => k === "log.level")) {
    entries.push(["log.level", nested]);
  }
  const out = fromEntries(entries);
  const logKeys = Object.keys((obj as { log?: object }).log ?? {});
  if (nested !== undefined && logKeys.length === 1 && normalizeLevel(nested)) {
    out.fields = out.fields.filter(([k]) => k !== "log");
  }
  return out;
}

const LOGFMT_PAIR = /(?<=^|\s)([^\s="]+)=("(?:[^"\\]|\\.)*"|[^\s"]*)/g;

function parseLogfmt(body: string): RuntimeLog | null {
  if (!/^[^\s="]+=/.test(body)) return null;
  const entries: [string, unknown][] = [];
  for (const m of body.matchAll(LOGFMT_PAIR)) {
    let value = m[2] ?? "";
    if (value.startsWith('"')) {
      try {
        value = JSON.parse(value) as string;
      } catch {
        value = value.slice(1, -1);
      }
    }
    entries.push([m[1] ?? "", value]);
  }
  const known = entries.some(([k]) => k === "msg" || k === "level" || k === "time");
  return entries.length >= 2 && known ? fromEntries(entries) : null;
}

function parsePlain(body: string): RuntimeLog {
  let text = body.trim();
  const ts = TEXT_TIME.exec(text);
  const time = ts ? parseTime(ts[1]) : null;
  if (ts && time) text = text.slice(ts[0].length);
  const { level, rest } = detectLevel(text);
  const message = rest.trim();
  const http = detectHttpText(message);
  return {
    time,
    level: level ?? (http ? levelFromStatus(http.status) : null),
    message,
    fields: [],
    http,
  };
}

/**
 * parseRuntimeLine parses one container log line. It accepts an optional
 * leading Docker RFC3339Nano timestamp, then JSON, logfmt, or plain text.
 */
export function parseRuntimeLine(text: string): RuntimeLog {
  const docker = DOCKER_TIME.exec(text);
  const body = docker ? text.slice(docker[0].length) : text;
  const parsed = parseJson(body.trim()) ?? parseLogfmt(body.trim()) ?? parsePlain(body);
  const dockerTime = docker ? parseTime(docker[1]) : null;
  return { ...parsed, time: dockerTime ?? parsed.time };
}

const BUILDKIT = /^#(\d+)(?: (.*))?$/;
const BUILDKIT_DONE = /^DONE(?: (\d+(?:\.\d+)?)s)?$/;
const BUILDKIT_ELAPSED = /^(\d+\.\d+)(?: (.*))?$/;

/**
 * parseBuildLine parses one line of build output: a shed "==> " heading, a
 * BuildKit plain-progress line, or anything else.
 */
export function parseBuildLine(text: string): BuildLine {
  if (text.startsWith("==> ")) return { kind: "step", text: text.slice(4) };

  const bk = BUILDKIT.exec(text);
  if (bk) {
    const id = Number(bk[1]);
    const rest = bk[2] ?? "";
    const line = (
      text: string,
      state: "done" | "cached" | "error" | null,
      elapsed: string | null,
    ) => ({ kind: "buildkit", id, text, state, elapsed }) as const;

    const done = BUILDKIT_DONE.exec(rest);
    if (done) return line("", "done", done[1] ? `${done[1]}s` : null);
    if (rest === "CACHED") return line("", "cached", null);
    if (rest.startsWith("ERROR")) return line(rest.replace(/^ERROR:?\s*/, ""), "error", null);
    const elapsed = BUILDKIT_ELAPSED.exec(rest);
    if (elapsed) return line(elapsed[2] ?? "", null, `${elapsed[1]}s`);
    return line(rest, null, null);
  }

  return { kind: "plain", text, level: detectLevel(text).level };
}
