import { Button, InputGroup, Loader, Select, Tabs } from "@cloudflare/kumo";
import { MagnifyingGlassIcon } from "@phosphor-icons/react";
import { useInfiniteQuery, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import {
  auditQuery,
  auditSettingsQuery,
  useSaveAuditSettings,
  type AuditFilters,
} from "../../api/audit";
import { errorMessage } from "../../api/client";
import { projectsQuery } from "../../api/projects";
import type { AuditEvent, AuditOutcome } from "../../api/types";
import { CountChip, Pill } from "../../components/Pill";
import {
  RecordEmpty,
  RecordId,
  RecordList,
  RecordMeta,
  RecordNote,
  RecordRow,
  RecordText,
} from "../../components/RecordList";
import { SettingRow } from "../../components/Settings";
import type { Tone } from "../../components/tone";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./Audit.module.css";
import { actionGroups, detailPairs } from "./detail";

const outcomeLooks: Record<AuditOutcome, { tone: Tone; label: string }> = {
  ok: { tone: "grass", label: "OK" },
  failed: { tone: "tomato", label: "Failed" },
  denied: { tone: "sunflower", label: "Denied" },
};

const COLUMNS = "minmax(0, 200px) minmax(0, 1fr) 80px 76px minmax(0, 150px) 100px";
const NARROW = "minmax(0, 1fr) auto";

const ANY = "any";

const noFilters: AuditFilters = { actor: "", action: "", service: "", outcome: "" };

/** AuditLog lists the audit log with filters, a page at a time, and its retention. */
export function AuditLog() {
  const [filters, setFilters] = useState(noFilters);
  const [actor, setActor] = useState("");
  const set = <K extends keyof AuditFilters>(key: K, value: AuditFilters[K]) =>
    setFilters((f) => ({ ...f, [key]: value }));

  // Typing a login filters once it settles, not on every keystroke.
  useEffect(() => {
    const t = setTimeout(() => setFilters((f) => ({ ...f, actor: actor.trim() })), 300);
    return () => clearTimeout(t);
  }, [actor]);

  const { data: projects = [] } = useQuery(projectsQuery);
  const services = useMemo(
    () =>
      new Map(
        projects.flatMap((p) => p.services.map((s) => [s.id, `${p.name} / ${s.name}`] as const)),
      ),
    [projects],
  );
  const events = useInfiniteQuery(auditQuery(filters));
  const rows = events.data?.pages.flatMap((p) => p.events) ?? [];
  const filtered = Object.values(filters).some(Boolean);

  return (
    <div className={styles.page}>
      <div className={styles.filters}>
        <InputGroup size="sm" className={styles.actor}>
          <InputGroup.Addon>
            <MagnifyingGlassIcon size={14} />
          </InputGroup.Addon>
          <InputGroup.Input
            aria-label="Actor"
            placeholder="Login or kind"
            value={actor}
            onChange={(e) => setActor(e.target.value)}
          />
        </InputGroup>
        <Select
          size="sm"
          aria-label="Action"
          className={styles.select}
          value={filters.action || ANY}
          onValueChange={(v) => set("action", v === ANY ? "" : String(v))}
          items={[
            { value: ANY, label: "Any action" },
            ...actionGroups.map((g) => ({ value: g as string, label: `${g}.*` })),
          ]}
        />
        <Select
          size="sm"
          aria-label="Service"
          className={styles.select}
          value={filters.service || ANY}
          onValueChange={(v) => set("service", v === ANY ? "" : String(v))}
          items={[
            { value: ANY, label: "Any service" },
            ...[...services].map(([id, name]) => ({ value: id, label: name })),
          ]}
        />
        <Tabs
          variant="segmented"
          size="sm"
          value={filters.outcome || ANY}
          onValueChange={(v) => set("outcome", v === ANY ? "" : (v as AuditOutcome))}
          tabs={[
            { value: ANY, label: "All" },
            { value: "ok", label: "OK" },
            { value: "failed", label: "Failed" },
            { value: "denied", label: "Denied" },
          ]}
        />
      </div>
      <RecordList
        title={
          <>
            <HelpTip topic="audit">Events</HelpTip>
          </>
        }
        actions={
          filtered && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setActor("");
                setFilters(noFilters);
              }}
            >
              Clear filters
            </Button>
          )
        }
        footer={
          events.hasNextPage && (
            <Button
              size="sm"
              loading={events.isFetchingNextPage}
              onClick={() => void events.fetchNextPage()}
            >
              Load more
            </Button>
          )
        }
      >
        {events.isPending ? (
          <div className={styles.loading}>
            <Loader size="sm" />
          </div>
        ) : events.isError ? (
          <p className={styles.error}>{errorMessage(events.error)}</p>
        ) : rows.length === 0 ? (
          <RecordEmpty>
            {filtered
              ? "Nothing in the log matches these filters."
              : "No events yet. Changes, sign-ins, denials, and agent calls appear here as they happen."}
          </RecordEmpty>
        ) : (
          rows.map((e) => <AuditRow key={e.id} event={e} service={services.get(e.serviceId)} />)
        )}
      </RecordList>
      <Retention />
    </div>
  );
}

function AuditRow({ event: e, service }: { event: AuditEvent; service?: string }) {
  const look = outcomeLooks[e.outcome];
  const count = typeof e.detail.count === "number" && e.detail.count > 1 ? e.detail.count : 0;
  const serviceName = service ?? (typeof e.detail.service === "string" ? e.detail.service : "");
  const client = typeof e.detail.client === "string" ? e.detail.client : "";
  const pairs = detailPairs(e.detail);
  return (
    <RecordRow columns={COLUMNS} narrowColumns={NARROW}>
      <RecordId className={styles.action} title={e.action}>
        {e.action}
        {count > 0 && <CountChip>×{count}</CountChip>}
      </RecordId>
      <RecordText optional className={styles.subject}>
        {serviceName}
      </RecordText>
      <span className={styles.outcome} data-tone={look.tone}>
        <span className={styles.bar} />
        {look.label}
      </span>
      <RecordMeta optional>
        <Pill>{e.actorKind}</Pill>
      </RecordMeta>
      <RecordMeta optional>
        {e.actor && `by ${e.actor}`}
        {client && ` via ${client}`}
      </RecordMeta>
      <RecordMeta optional className={styles.when} title={formatDate(e.ts)}>
        <time dateTime={e.ts}>{relativeTime(e.ts)}</time>
      </RecordMeta>
      {pairs.length > 0 && (
        <RecordNote>
          <dl className={styles.detail}>
            {pairs.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
        </RecordNote>
      )}
    </RecordRow>
  );
}

function Retention() {
  const { data } = useSuspenseQuery(auditSettingsQuery);
  const save = useSaveAuditSettings();
  const [days, setDays] = useState(data.retentionDays);
  const invalid = !Number.isInteger(days) || days < 1 || days > 3650;
  const dirty = days !== data.retentionDays;
  return (
    <form
      onSubmit={(ev) => {
        ev.preventDefault();
        if (dirty && !invalid) save.mutate({ retentionDays: days });
      }}
    >
      <SettingRow
        label="Keep events for"
        description={
          <>
            1 to 3650 days. The default is 365. Older events are deleted every hour.
            {save.error && <span className={styles.errorText}> {errorMessage(save.error)}</span>}
          </>
        }
        control={
          <>
            <InputGroup size="sm" className={styles.days}>
              <InputGroup.Input
                aria-label="Days to keep events"
                type="number"
                min={1}
                max={3650}
                value={Number.isNaN(days) ? "" : days}
                onChange={(ev) => setDays(ev.target.valueAsNumber)}
              />
              <InputGroup.Suffix>days</InputGroup.Suffix>
            </InputGroup>
            {dirty && (
              <>
                <Button
                  size="sm"
                  type="button"
                  disabled={save.isPending}
                  onClick={() => {
                    setDays(data.retentionDays);
                    save.reset();
                  }}
                >
                  Discard
                </Button>
                <Button
                  size="sm"
                  type="submit"
                  variant="primary"
                  loading={save.isPending}
                  disabled={invalid}
                >
                  Save
                </Button>
              </>
            )}
          </>
        }
      />
    </form>
  );
}
