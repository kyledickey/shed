import { Button, InputGroup, LayerCard, Loader, Tabs } from "@cloudflare/kumo";
import { ArrowSquareOutIcon, MagnifyingGlassIcon } from "@phosphor-icons/react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { errorMessage } from "../../api/client";
import {
  historyRanges,
  logHistoryQuery,
  traceHref,
  type HistoryFilters,
  type HistoryRange,
} from "../../api/logHistory";
import type { LogRecord } from "../../api/types";
import { RecordEmpty } from "../../components/RecordList";
import { formatDate } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./LogHistory.module.css";

const levelOptions = [
  { value: "any", label: "All" },
  { value: "error", label: "Error" },
  { value: "warn", label: "Warn" },
  { value: "info", label: "Info" },
  { value: "debug", label: "Debug" },
];

const errorLevels = new Set(["error", "fatal"]);

/** LogHistory searches the stored logs of a service across deployments. */
export function LogHistory({ serviceId, name }: { serviceId: string; name: string }) {
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<HistoryFilters>({ q: "", level: "", range: "1h" });

  // The search runs once typing settles.
  useEffect(() => {
    const t = setTimeout(() => setFilters((f) => ({ ...f, q: search.trim() })), 300);
    return () => clearTimeout(t);
  }, [search]);

  const history = useInfiniteQuery(logHistoryQuery(serviceId, filters));
  const records = history.data?.pages.flatMap((p) => p.records) ?? [];

  return (
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.heading}>
          <HelpTip topic="logHistory">{name} log history</HelpTip>
        </span>
        <div className={styles.filters}>
          <InputGroup size="sm" className={styles.search}>
            <InputGroup.Addon>
              <MagnifyingGlassIcon size={14} />
            </InputGroup.Addon>
            <InputGroup.Input
              aria-label="Search logs"
              placeholder='timeout "connection refused" user:42'
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </InputGroup>
          <Tabs
            variant="segmented"
            size="sm"
            value={filters.level || "any"}
            onValueChange={(v) =>
              setFilters((f) => ({
                ...f,
                level: v === "any" ? "" : (v as HistoryFilters["level"]),
              }))
            }
            tabs={levelOptions}
          />
          <Tabs
            variant="segmented"
            size="sm"
            value={filters.range}
            onValueChange={(v) => setFilters((f) => ({ ...f, range: v as HistoryRange }))}
            tabs={historyRanges.map((r) => ({ value: r, label: r }))}
          />
        </div>
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        {history.isPending ? (
          <div className={styles.loading}>
            <Loader size="sm" />
          </div>
        ) : history.isError ? (
          <p className={styles.error}>{errorMessage(history.error)}</p>
        ) : records.length === 0 ? (
          <RecordEmpty>
            Nothing stored in this time range matches. Logs are kept while telemetry is on.
          </RecordEmpty>
        ) : (
          <div className={styles.records}>
            {records.map((r, i) => (
              <Record key={`${r.time}-${i}`} record={r} />
            ))}
          </div>
        )}
        {history.hasNextPage && (
          <div className={styles.more}>
            <Button
              size="sm"
              loading={history.isFetchingNextPage}
              onClick={() => void history.fetchNextPage()}
            >
              Load older
            </Button>
          </div>
        )}
      </LayerCard.Primary>
    </LayerCard>
  );
}

function Record({ record: r }: { record: LogRecord }) {
  return (
    <div
      className={styles.record}
      data-error={errorLevels.has(r.level) || r.stream === "stderr" || undefined}
    >
      <time className={styles.time} dateTime={r.time} title={r.time}>
        {formatDate(r.time)}
      </time>
      <span className={styles.level}>{r.level}</span>
      <span className={styles.message}>{r.message}</span>
      {r.traceId && (
        <a
          className={styles.trace}
          href={traceHref(r.traceId)}
          target="_blank"
          rel="noreferrer"
          title="Open this trace in Grafana"
        >
          Trace <ArrowSquareOutIcon size={12} />
        </a>
      )}
    </div>
  );
}

/** LogMode is whether the Logs tab streams live output or searches stored logs. */
export type LogMode = "live" | "history";

/** LogModeTabs switches the Logs tab between live output and stored history. */
export function LogModeTabs({ mode, onChange }: { mode: LogMode; onChange: (m: LogMode) => void }) {
  return (
    <Tabs
      variant="segmented"
      size="sm"
      className={styles.mode}
      value={mode}
      onValueChange={(v) => onChange(v as LogMode)}
      tabs={[
        { value: "live", label: "Live" },
        { value: "history", label: "History" },
      ]}
    />
  );
}
