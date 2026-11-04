import { Button, InputGroup, LayerCard, Tabs, Tooltip } from "@cloudflare/kumo";
import { Dialog as BaseDialog } from "@cloudflare/kumo/primitives/dialog";
import {
  ArrowsInIcon,
  ArrowsOutIcon,
  CaretRightIcon,
  DownloadSimpleIcon,
  MagnifyingGlassIcon,
  PauseIcon,
  PlayIcon,
  TextAlignLeftIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import type { LogLine, StreamState } from "../../api/events";
import { cx } from "../../lib/cx";
import { parseRuntimeLine, type LogLevel, type RuntimeLog } from "../../lib/logs";
import { Histogram } from "../charts/Mini";
import { CopyButton } from "../Misc";
import { CountChip } from "../Pill";
import type { Tone } from "../tone";
import styles from "./logs.module.css";
import { Highlight, JumpToLatest, LogTime, StreamIndicator, useFollow } from "./shared";

type Entry = RuntimeLog & { id: number; raw: string };

const levels: { level: LogLevel; label: string; tone: Tone }[] = [
  { level: "error", label: "Error", tone: "tomato" },
  { level: "warn", label: "Warn", tone: "sunflower" },
  { level: "info", label: "Info", tone: "sky" },
  { level: "debug", label: "Debug", tone: "neutral" },
];

/** statusClass marks server errors in the danger color and redirects as quiet. */
function statusClass(status: number): string | undefined {
  if (status >= 500) return styles.statusError;
  if (status >= 300 && status < 400) return styles.statusQuiet;
  return undefined;
}

/** showMessage hides a message that only repeats the HTTP request line. */
function showMessage(e: Entry): boolean {
  if (!e.http) return true;
  return e.message !== "" && !e.message.includes(`${e.http.method} ${e.http.path}`);
}

/** useParsed parses lines once each, caching by line id. */
function useParsed(lines: LogLine[]): Entry[] {
  const cache = useRef(new Map<number, Entry>());
  return useMemo(() => {
    const next = new Map<number, Entry>();
    const out = lines.map((l) => {
      const hit = cache.current.get(l.id) ?? { ...parseRuntimeLine(l.text), id: l.id, raw: l.text };
      next.set(l.id, hit);
      return hit;
    });
    cache.current = next;
    return out;
  }, [lines]);
}

type RuntimeLogViewProps = {
  lines: LogLine[];
  state: StreamState;
  /** Shown in the fullscreen header. */
  title: ReactNode;
  onClear?: () => void;
  height?: number;
};

/**
 * RuntimeLogView renders parsed container logs with search, level filters,
 * follow mode, pause, and a fullscreen watch mode.
 */
export function RuntimeLogView({
  lines,
  state,
  title,
  onClear,
  height = 440,
}: RuntimeLogViewProps) {
  const [query, setQuery] = useState("");
  const [level, setLevel] = useState<LogLevel | "all">("all");
  const [wrap, setWrap] = useState(true);
  const [frozen, setFrozen] = useState<LogLine[] | null>(null);
  const [fullscreen, setFullscreen] = useState(false);

  const entries = useParsed(frozen ?? lines);
  const counts = useMemo(() => {
    const c: Record<LogLevel, number> = { error: 0, warn: 0, info: 0, debug: 0 };
    for (const e of entries) c[e.level ?? "info"]++;
    return c;
  }, [entries]);
  const visible = useMemo(() => {
    const q = query.toLowerCase();
    return entries.filter(
      (e) =>
        (level === "all" || (e.level ?? "info") === level) &&
        (!q || e.raw.toLowerCase().includes(q)),
    );
  }, [entries, level, query]);

  const download = () => {
    const blob = new Blob([entries.map((e) => e.raw).join("\n")], { type: "text/plain" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = "logs.txt";
    a.click();
    URL.revokeObjectURL(a.href);
  };

  const tool = (label: string, icon: ReactNode, onClick: () => void, pressed?: boolean) => (
    <Tooltip
      content={label}
      render={
        <Button
          variant="ghost"
          size="sm"
          shape="square"
          icon={icon}
          aria-label={label}
          aria-pressed={pressed}
          className={cx(pressed && styles.pressed)}
          onClick={onClick}
        />
      }
    />
  );

  const toolbar = (inFullscreen: boolean) => (
    <div className={styles.toolbar}>
      <InputGroup size="sm" className={styles.search}>
        <InputGroup.Addon>
          <MagnifyingGlassIcon size={14} />
        </InputGroup.Addon>
        <InputGroup.Input
          aria-label="Search logs"
          placeholder="Search logs"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          spellCheck={false}
        />
        {query && (
          <InputGroup.Addon align="end">
            <CountChip>{visible.length}</CountChip>
          </InputGroup.Addon>
        )}
      </InputGroup>
      <Tabs
        variant="segmented"
        size="sm"
        aria-label="Level"
        value={level}
        onValueChange={(v) => setLevel(v as LogLevel | "all")}
        tabs={[
          { value: "all", label: "All" },
          ...levels.map((l) => ({
            value: l.level,
            label: (
              <>
                {l.label}
                <span className={styles.levelCount}>{counts[l.level]}</span>
              </>
            ),
          })),
        ]}
      />
      <div className={styles.toolbarEnd}>
        <StreamIndicator state={state} paused={frozen !== null} />
        {tool(
          frozen ? "Resume" : "Pause",
          frozen ? <PlayIcon size={16} /> : <PauseIcon size={16} />,
          () => setFrozen(frozen ? null : lines),
        )}
        {tool(
          wrap ? "Don't wrap lines" : "Wrap lines",
          <TextAlignLeftIcon size={16} />,
          () => setWrap(!wrap),
          wrap,
        )}
        {tool("Download", <DownloadSimpleIcon size={16} />, download)}
        {onClear && tool("Clear", <TrashIcon size={16} />, onClear)}
        {tool(
          inFullscreen ? "Exit fullscreen" : "Fullscreen",
          inFullscreen ? <ArrowsInIcon size={16} /> : <ArrowsOutIcon size={16} />,
          () => setFullscreen(!inFullscreen),
        )}
      </div>
    </div>
  );

  return (
    <>
      <LayerCard className={styles.viewer}>
        <LayerCard.Secondary className={styles.bar}>{toolbar(false)}</LayerCard.Secondary>
        <LayerCard.Primary className={styles.panel}>
          <LogList entries={visible} query={query} wrap={wrap} style={{ height }} />
        </LayerCard.Primary>
      </LayerCard>

      <BaseDialog.Root open={fullscreen} onOpenChange={setFullscreen}>
        <BaseDialog.Portal>
          <BaseDialog.Backdrop className={styles.fsBackdrop} />
          <BaseDialog.Popup className={styles.fullscreen}>
            <header className={styles.fsHeader}>
              <BaseDialog.Title className={styles.fsTitle}>{title}</BaseDialog.Title>
              <VolumeChart entries={entries} />
            </header>
            <div className={styles.bar}>{toolbar(true)}</div>
            <div className={cx(styles.panel, styles.fsPanel)}>
              <LogList entries={visible} query={query} wrap={wrap} className={styles.fsList} />
            </div>
          </BaseDialog.Popup>
        </BaseDialog.Portal>
      </BaseDialog.Root>
    </>
  );
}

const MAX_RENDERED = 1500;

function LogList({
  entries,
  query,
  wrap,
  className,
  style,
}: {
  entries: Entry[];
  query: string;
  wrap: boolean;
  className?: string;
  style?: CSSProperties;
}) {
  const { ref, onScroll, following, unseen, jump } = useFollow(entries.length);
  const [open, setOpen] = useState<number | null>(null);
  const shown = entries.length > MAX_RENDERED ? entries.slice(-MAX_RENDERED) : entries;
  return (
    <div className={cx(styles.listWrap, className)} style={style}>
      <div
        ref={ref}
        onScroll={onScroll}
        className={cx(styles.list, !wrap && styles.nowrap)}
        role="log"
        aria-live="off"
      >
        {shown.length === 0 && <div className={styles.listEmpty}>No matching log lines yet.</div>}
        {shown.map((e) => (
          <RuntimeRow
            key={e.id}
            entry={e}
            query={query}
            open={open === e.id}
            onToggle={() => setOpen(open === e.id ? null : e.id)}
          />
        ))}
      </div>
      {!following && <JumpToLatest unseen={unseen} onClick={jump} />}
    </div>
  );
}

function RuntimeRow({
  entry: e,
  query,
  open,
  onToggle,
}: {
  entry: Entry;
  query: string;
  open: boolean;
  onToggle: () => void;
}) {
  const expandable = e.fields.length > 0 || e.raw.length > 0;
  return (
    <div className={styles.row} data-level={e.level ?? undefined} data-open={open || undefined}>
      <button type="button" className={styles.rowMain} onClick={onToggle} disabled={!expandable}>
        <LogTime time={e.time} />
        <span className={styles.level}>{e.level ?? ""}</span>
        <span className={styles.body}>
          {e.http && (
            <span className={styles.http}>
              <span className={styles.method}>{e.http.method}</span>
              <span className={cx(styles.status, statusClass(e.http.status))}>{e.http.status}</span>
              <span className={styles.path}>
                <Highlight text={e.http.path} query={query} />
              </span>
              {e.http.duration && <span className={styles.duration}>{e.http.duration}</span>}
            </span>
          )}
          {showMessage(e) && (
            <span className={styles.message}>
              <Highlight text={e.message} query={query} />
            </span>
          )}
          {e.fields.map(([k, v]) => (
            <span key={k} className={styles.field}>
              <span className={styles.fieldKey}>{k}</span>
              <span className={styles.fieldEq}>=</span>
              <span className={styles.fieldValue}>
                <Highlight text={v} query={query} />
              </span>
            </span>
          ))}
        </span>
        <CaretRightIcon size={12} className={styles.rowChevron} />
      </button>
      {open && (
        <div className={styles.detail}>
          {e.fields.length > 0 && (
            <dl className={styles.detailFields}>
              {e.http && (
                <>
                  <dt>request</dt>
                  <dd>
                    {e.http.method} {e.http.path} → {e.http.status}
                    {e.http.duration && ` in ${e.http.duration}`}
                  </dd>
                </>
              )}
              {e.fields.map(([k, v]) => (
                <div key={k} className={styles.detailField}>
                  <dt>{k}</dt>
                  <dd>{v}</dd>
                </div>
              ))}
            </dl>
          )}
          <div className={styles.raw}>
            <code>{e.raw}</code>
            <CopyButton value={e.raw} label="Copy line" />
          </div>
        </div>
      )}
    </div>
  );
}

const BUCKETS = 60;

/** VolumeChart buckets log lines over time by level. */
function VolumeChart({ entries }: { entries: Entry[] }) {
  const [hover, setHover] = useState<number | null>(null);
  const timed = entries.filter((e) => e.time);
  if (timed.length < 2) return null;
  const t0 = timed[0]!.time!.getTime();
  const t1 = timed.at(-1)!.time!.getTime();
  const span = Math.max(1, t1 - t0);
  const buckets = Array.from({ length: BUCKETS }, () => ({ error: 0, warn: 0, info: 0, debug: 0 }));
  for (const e of timed) {
    const i = Math.min(BUCKETS - 1, Math.floor(((e.time!.getTime() - t0) / span) * BUCKETS));
    buckets[i]![e.level ?? "info"]++;
  }
  const at = hover !== null ? buckets[hover]! : null;
  const fmt = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
  return (
    <div className={styles.volume}>
      <div className={styles.volumeLabel}>
        {at && hover !== null ? (
          <>
            <span>{fmt.format(new Date(t0 + (hover / BUCKETS) * span))}</span>
            {levels.map((l) => (
              <span key={l.level} className={styles.volumeStat} data-tone={l.tone}>
                {at[l.level]}
              </span>
            ))}
          </>
        ) : (
          <span>
            {timed.length} lines over {Math.max(1, Math.round(span / 1000))}s
          </span>
        )}
      </div>
      <Histogram
        buckets={buckets}
        keys={[
          { key: "debug", tone: "neutral" },
          { key: "info", tone: "sky" },
          { key: "warn", tone: "sunflower" },
          { key: "error", tone: "tomato" },
        ]}
        height={36}
        onHover={setHover}
      />
    </div>
  );
}
