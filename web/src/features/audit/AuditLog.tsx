import { useInfiniteQuery, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Ban, Check, ScrollText, Search, X } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import {
  auditQuery,
  auditSettingsQuery,
  useSaveAuditSettings,
  type AuditFilters,
} from "../../api/audit";
import { errorMessage } from "../../api/client";
import { projectsQuery } from "../../api/projects";
import type { AuditEvent, AuditOutcome } from "../../api/types";
import { Badge, Spinner } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input, Segmented, Select } from "../../components/Form";
import { List, Stack } from "../../components/Layout";
import { EmptyState } from "../../components/Misc";
import { Tooltip } from "../../components/Overlay";
import type { Tone } from "../../components/tone";
import { formatDate, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { SettingsCard } from "../settings/SectionForm";
import styles from "./Audit.module.css";
import { actionGroups, detailPairs } from "./detail";

const outcomeLooks: Record<AuditOutcome, { tone: Tone; label: string; icon: ReactNode }> = {
  ok: { tone: "grass", label: "OK", icon: <Check size={14} /> },
  failed: { tone: "tomato", label: "Failed", icon: <X size={14} /> },
  denied: { tone: "sunflower", label: "Denied", icon: <Ban size={13} /> },
};

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
    <Stack gap={4}>
      <LayerCard
        header={
          <div className={styles.toolbar}>
            <span className={styles.heading}>
              Audit log <HelpTip topic="audit" />
            </span>
            <Input
              aria-label="Actor"
              className={styles.actor}
              prefix={<Search size={13} />}
              placeholder="Login or kind"
              value={actor}
              onChange={(e) => setActor(e.target.value)}
            />
            <Select
              value={filters.action}
              onChange={(v) => set("action", v)}
              placeholder="Any action"
              mono
              options={[
                { value: "", label: "Any action" },
                ...actionGroups.map((g) => ({ value: g, label: `${g}.*` })),
              ]}
            />
            <Select
              value={filters.service}
              onChange={(v) => set("service", v)}
              placeholder="Any service"
              options={[
                { value: "", label: "Any service" },
                ...[...services].map(([id, name]) => ({ value: id, label: name })),
              ]}
            />
            <Segmented
              size="sm"
              label="Outcome"
              value={filters.outcome || "any"}
              onChange={(v) => set("outcome", v === "any" ? "" : v)}
              options={[
                { value: "any", label: "All" },
                { value: "ok", label: "OK" },
                { value: "failed", label: "Failed" },
                { value: "denied", label: "Denied" },
              ]}
            />
          </div>
        }
        sheetClassName={styles.sheet}
        footer={
          events.hasNextPage && (
            <div className={styles.more}>
              <Button
                size="sm"
                loading={events.isFetchingNextPage}
                onClick={() => void events.fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          )
        }
      >
        {events.isPending ? (
          <div className={styles.loading}>
            <Spinner />
          </div>
        ) : events.isError ? (
          <p className={styles.error}>{errorMessage(events.error)}</p>
        ) : rows.length === 0 ? (
          <EmptyState
            icon={<ScrollText />}
            tone="neutral"
            title={filtered ? "No matching events" : "No events yet"}
            description={
              filtered
                ? "Nothing in the log matches these filters."
                : "Changes, sign-ins, denials, and agent calls appear here as they happen."
            }
            actions={
              filtered && (
                <Button
                  size="sm"
                  onClick={() => {
                    setActor("");
                    setFilters(noFilters);
                  }}
                >
                  Clear filters
                </Button>
              )
            }
          />
        ) : (
          <List>
            {rows.map((e) => (
              <AuditRow key={e.id} event={e} service={services.get(e.serviceId)} />
            ))}
          </List>
        )}
      </LayerCard>
      <RetentionCard />
    </Stack>
  );
}

function AuditRow({ event: e, service }: { event: AuditEvent; service?: string }) {
  const look = outcomeLooks[e.outcome];
  const count = typeof e.detail.count === "number" && e.detail.count > 1 ? e.detail.count : 0;
  const serviceName = service ?? (typeof e.detail.service === "string" ? e.detail.service : "");
  const client = typeof e.detail.client === "string" ? e.detail.client : "";
  return (
    <div className={styles.row}>
      <Tooltip content={look.label}>
        <span className={styles.rail} data-tone={look.tone} aria-label={look.label}>
          {look.icon}
        </span>
      </Tooltip>
      <div className={styles.body}>
        <span className={styles.title}>
          <span className={styles.action}>{e.action}</span>
          {serviceName && <span className={styles.subject}>{serviceName}</span>}
          {count > 0 && (
            <Badge size="sm" tone="sunflower">
              ×{count}
            </Badge>
          )}
        </span>
        <span className={styles.meta}>
          <Badge size="sm" variant="outline">
            {e.actorKind}
          </Badge>
          {e.actor && <span className={styles.who}>{e.actor}</span>}
          {client && <span>via {client}</span>}
        </span>
        <Detail event={e} />
      </div>
      <Tooltip content={formatDate(e.ts)}>
        <time className={styles.when} dateTime={e.ts}>
          {relativeTime(e.ts)}
        </time>
      </Tooltip>
    </div>
  );
}

function Detail({ event }: { event: AuditEvent }) {
  const pairs = detailPairs(event.detail);
  if (pairs.length === 0) return null;
  return (
    <dl className={styles.detail}>
      {pairs.map(([k, v]) => (
        <div key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  );
}

function RetentionCard() {
  const { data } = useSuspenseQuery(auditSettingsQuery);
  const save = useSaveAuditSettings();
  const [days, setDays] = useState(data.retentionDays);
  const invalid = !Number.isInteger(days) || days < 1 || days > 3650;
  return (
    <SettingsCard
      title="Retention"
      meta="Older events are deleted every hour."
      form={{
        dirty: days !== data.retentionDays,
        invalid,
        pending: save.isPending,
        error: save.error,
        reset: () => {
          setDays(data.retentionDays);
          save.reset();
        },
        save: () => save.mutate({ retentionDays: days }),
      }}
    >
      <Field label="Keep events for" hint="1 to 3650 days. The default is 365.">
        {(id) => (
          <Input
            id={id}
            className={styles.days}
            type="number"
            min={1}
            max={3650}
            mono
            suffix="days"
            value={days}
            onChange={(ev) => setDays(Number(ev.target.value))}
          />
        )}
      </Field>
    </SettingsCard>
  );
}
