import { useInfiniteQuery } from "@tanstack/react-query";
import { ExternalLink, ScrollText, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { errorMessage } from "../../api/client";
import {
  historyRanges,
  logHistoryQuery,
  traceHref,
  type HistoryFilters,
} from "../../api/logHistory";
import type { LogRecord } from "../../api/types";
import { Badge, Spinner } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Input, Segmented } from "../../components/Form";
import { EmptyState } from "../../components/Misc";
import type { Tone } from "../../components/tone";
import { formatDate } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./LogHistory.module.css";

const levelTones: Record<string, Tone> = {
  error: "tomato",
  fatal: "tomato",
  warn: "sunflower",
  warning: "sunflower",
  info: "neutral",
  debug: "neutral",
};

const levelOptions = [
  { value: "", label: "All" },
  { value: "debug", label: "Debug" },
  { value: "info", label: "Info" },
  { value: "warn", label: "Warn" },
  { value: "error", label: "Error" },
] as const;

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
    <LayerCard
      header={
        <div className={styles.toolbar}>
          <span className={styles.heading}>
            {name} log history <HelpTip topic="logHistory" />
          </span>
          <Input
            aria-label="Search logs"
            className={styles.search}
            prefix={<Search size={13} />}
            placeholder='timeout "connection refused" user:42'
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Segmented
            size="sm"
            label="Level"
            value={filters.level}
            onChange={(level) => setFilters((f) => ({ ...f, level }))}
            options={[...levelOptions]}
          />
          <Segmented
            size="sm"
            label="Time range"
            value={filters.range}
            onChange={(range) => setFilters((f) => ({ ...f, range }))}
            options={historyRanges.map((r) => ({ value: r, label: r }))}
          />
        </div>
      }
      sheetClassName={styles.sheet}
      footer={
        history.hasNextPage && (
          <div className={styles.more}>
            <Button
              size="sm"
              loading={history.isFetchingNextPage}
              onClick={() => void history.fetchNextPage()}
            >
              Load older
            </Button>
          </div>
        )
      }
    >
      {history.isPending ? (
        <div className={styles.loading}>
          <Spinner />
        </div>
      ) : history.isError ? (
        <p className={styles.error}>{errorMessage(history.error)}</p>
      ) : records.length === 0 ? (
        <EmptyState
          icon={<ScrollText />}
          tone="neutral"
          title="No matching records"
          description="Nothing stored in this time range matches. Logs are kept while telemetry is on."
        />
      ) : (
        <div className={styles.records}>
          {records.map((r, i) => (
            <Record key={`${r.time}-${i}`} record={r} />
          ))}
        </div>
      )}
    </LayerCard>
  );
}

function Record({ record: r }: { record: LogRecord }) {
  return (
    <div className={styles.record} data-stream={r.stream}>
      <time className={styles.time} dateTime={r.time} title={r.time}>
        {formatDate(r.time)}
      </time>
      {r.level ? (
        <Badge size="sm" tone={levelTones[r.level] ?? "neutral"} variant="soft">
          {r.level}
        </Badge>
      ) : (
        <span />
      )}
      <span className={styles.message}>{r.message}</span>
      {r.traceId && (
        <a
          className={styles.trace}
          href={traceHref(r.traceId)}
          target="_blank"
          rel="noreferrer"
          title="Open this trace in Grafana"
        >
          Trace <ExternalLink size={12} />
        </a>
      )}
    </div>
  );
}
