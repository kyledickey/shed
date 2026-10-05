import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { ServiceKind } from "../../../../api/types";
import { Button } from "../../../../components/Button";
import { Field, Input, Select } from "../../../../components/Form";
import { Callout, ServiceIcon } from "../../../../components/Misc";
import { isValidKey } from "../../../../lib/env";
import { formatBytes } from "../../../../lib/format";
import { serviceEdges } from "../../../projects/graph";
import { Demo } from "../../kit";
import {
  buildScope,
  injectedVars,
  MAX_RESOLVED_BYTES,
  resolve,
  resolveKey,
  type Scope,
  type ServiceFacts,
  type Target,
} from "../../lib/vars";
import shared from "../pages.module.css";
import styles from "./Playground.module.css";
import { rowId, VarRow, type Origin } from "./VarRow";

type PlayService = ServiceFacts & { kind: ServiceKind; commitSha: string };
type PlayProject = { name: string; services: PlayService[] };
type Edits = Record<string, Record<string, string>>;

const SAMPLE: PlayProject = {
  name: "sample",
  services: [
    {
      name: "web",
      kind: "app",
      port: 8080,
      branch: "main",
      domains: ["web-sample.apps.example.com"],
      commitSha: "3f9c2ab41d0e8a7b5c6d9e0f1a2b3c4d5e6f7a8b",
      own: {
        DATABASE_URL: "${{ postgres.DATABASE_URL }}",
        CACHE_URL: "${{ redis.REDIS_URL }}",
        PUBLIC_URL: "https://${{ SHED_PUBLIC_DOMAIN }}",
      },
    },
    {
      name: "postgres",
      kind: "postgres",
      port: 5432,
      branch: "",
      domains: [],
      commitSha: "",
      own: {
        POSTGRES_USER: "postgres",
        POSTGRES_PASSWORD: "sample-password",
        POSTGRES_DB: "app",
        DATABASE_URL:
          "postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}",
      },
    },
    {
      name: "redis",
      kind: "redis",
      port: 6379,
      branch: "",
      domains: [],
      commitSha: "",
      own: {
        REDIS_PASSWORD: "sample-password",
        REDIS_URL: "redis://default:${{REDIS_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:6379",
      },
    },
  ],
};

/**
 * Playground resolves the variables of a sample project the way a deploy
 * would. Edits are scratch space in the browser.
 */
export function Playground() {
  return (
    <Demo title="Resolver playground">
      <Sandbox project={SAMPLE} />
    </Demo>
  );
}

function Sandbox({ project }: { project: PlayProject }) {
  const [selfName, setSelfName] = useState("");
  const [edits, setEdits] = useState<Edits>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [focus, setFocus] = useState<Target | null>(null);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");

  const selected = project.services.find((s) => s.name === selfName) ?? project.services[0]!;
  const facts = useMemo(
    () => project.services.map((s) => ({ ...s, own: { ...s.own, ...edits[s.name] } })),
    [project, edits],
  );
  const scope = useMemo(
    () => buildScope(project.name, facts, selected.name, selected.commitSha),
    [project.name, facts, selected],
  );
  const overall = useMemo(() => resolve(selected.name, scope), [selected.name, scope]);
  const edges = useMemo(
    () =>
      serviceEdges(
        facts.map((s) => ({ id: s.name, name: s.name })),
        facts.map((s) => s.own),
      ),
    [facts],
  );
  const edited = Object.values(edits).some((e) => Object.keys(e).length > 0);

  useEffect(() => {
    if (focus)
      document
        .getElementById(rowId(focus))
        ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [focus]);

  const setValue = (service: string, key: string, value: string) =>
    setEdits((prev) => ({ ...prev, [service]: { ...prev[service], [key]: value } }));
  const unset = (service: string, key: string) =>
    setEdits((prev) => ({
      ...prev,
      [service]: Object.fromEntries(Object.entries(prev[service] ?? {}).filter(([k]) => k !== key)),
    }));
  const jump = (t: Target) => {
    if (t.service !== selected.name) setOpen((prev) => new Set(prev).add(t.service));
    setFocus(t);
  };

  const addScratch = () => {
    setValue(selected.name, newKey, newValue);
    setNewKey("");
    setNewValue("");
  };

  return (
    <>
      <div className={styles.toolbar}>
        <div className={styles.field}>
          <Field label="Resolve for service">
            {(id) => (
              <Select
                id={id}
                value={selected.name}
                onChange={(v) => {
                  setSelfName(v);
                  setFocus(null);
                }}
                options={project.services.map((s) => ({ value: s.name, label: s.name }))}
              />
            )}
          </Field>
        </div>
        {edited && (
          <Button size="sm" variant="ghost" onClick={() => setEdits({})}>
            Discard edits
          </Button>
        )}
      </div>
      <p className={styles.scratch}>
        Edits here are scratch space in this browser tab. Nothing is saved. The sample values are
        made up.
      </p>

      {overall.ok ? (
        <Summary values={overall.value} />
      ) : (
        <div className={styles.banner}>
          <Callout tone="tomato" title="This set would fail the deployment">
            <code>{overall.error}</code>
          </Callout>
        </div>
      )}

      {facts.map((svc) => {
        const isSelf = svc.name === selected.name;
        const isOpen = isSelf || open.has(svc.name);
        return (
          <div key={svc.name} className={styles.group}>
            {isSelf ? (
              <div className={styles.groupHead}>
                <ServiceIcon kind={svc.kind} size={18} />
                {svc.name}: resolved for this service
              </div>
            ) : (
              <button
                type="button"
                className={styles.serviceToggle}
                aria-expanded={isOpen}
                onClick={() =>
                  setOpen((prev) => {
                    const next = new Set(prev);
                    if (!next.delete(svc.name)) next.add(svc.name);
                    return next;
                  })
                }
              >
                {isOpen ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                <ServiceIcon kind={svc.kind} size={18} />
                {svc.name}
                <span className={styles.serviceMeta}>{Object.keys(svc.own).length} variables</span>
              </button>
            )}
            {isOpen && (
              <ServiceRows
                service={svc}
                project={project}
                original={project.services.find((s) => s.name === svc.name)!}
                scope={scope}
                focus={focus}
                editing={editing}
                setEditing={setEditing}
                setValue={setValue}
                unset={unset}
                jump={jump}
                sha={isSelf ? selected.commitSha : ""}
              />
            )}
          </div>
        );
      })}

      <div className={styles.add}>
        <Field label={`Add a scratch variable to ${selected.name}`}>
          {(id) => (
            <Input
              id={id}
              mono
              placeholder="KEY"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value)}
            />
          )}
        </Field>
        <Field label="Value">
          {(id) => (
            <Input
              id={id}
              mono
              placeholder="${{ other.KEY }}"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
            />
          )}
        </Field>
        <Button disabled={!isValidKey(newKey)} onClick={addScratch}>
          Add
        </Button>
      </div>
      <p className={styles.hint}>
        Try <code>A=${"{{ B }}"}</code> and <code>B=${"{{ A }}"}</code> to see the cycle error.
      </p>

      {edges.length > 0 && (
        <ul className={styles.deps} aria-label="References between services">
          {edges.map((e) => (
            <li key={`${e.from}>${e.to}`} className={styles.dep}>
              {e.from} references {e.to}
              {e.label ? ` (${e.label})` : ""}
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

function Summary({ values }: { values: Record<string, string> }) {
  const enc = new TextEncoder();
  const bytes = Object.values(values).reduce((n, v) => n + enc.encode(v).length, 0);
  return (
    <div className={shared.stats}>
      <div className={shared.stat}>
        <span className={shared.statLabel}>Environment</span>
        <span className={shared.statValue}>{Object.keys(values).length}</span>
        <span className={shared.statDetail}>variables in the container</span>
      </div>
      <div className={shared.stat}>
        <span className={shared.statLabel}>Resolved size</span>
        <span className={shared.statValue}>{formatBytes(bytes)}</span>
        <span className={shared.statDetail}>of {formatBytes(MAX_RESOLVED_BYTES)} allowed</span>
      </div>
    </div>
  );
}

function ServiceRows({
  service,
  project,
  original,
  scope,
  focus,
  editing,
  setEditing,
  setValue,
  unset,
  jump,
  sha,
}: {
  service: PlayService;
  project: PlayProject;
  original: PlayService;
  scope: Scope;
  focus: Target | null;
  editing: string | null;
  setEditing: (id: string | null) => void;
  setValue: (service: string, key: string, value: string) => void;
  unset: (service: string, key: string) => void;
  jump: (t: Target) => void;
  sha: string;
}) {
  const injected = injectedVars(project.name, service, sha);
  const vars = scope[service.name] ?? {};
  const stored = Object.keys(service.own).sort();
  const provided = Object.keys(injected)
    .filter((k) => !Object.hasOwn(service.own, k))
    .sort();

  const row = (key: string, origin: Origin) => {
    const id = rowId({ service: service.name, key });
    return (
      <VarRow
        key={key}
        service={service.name}
        name={key}
        raw={vars[key] ?? ""}
        origin={origin}
        overridesInjected={origin !== "injected" && Object.hasOwn(injected, key)}
        scope={scope}
        resolved={resolveKey(service.name, key, scope)}
        focused={focus?.service === service.name && focus.key === key}
        editing={editing === id}
        onEditToggle={() => setEditing(editing === id ? null : id)}
        onChange={(v) => setValue(service.name, key, v)}
        onReset={() => unset(service.name, key)}
        onJump={jump}
      />
    );
  };
  const originOf = (key: string): Origin => {
    if (!Object.hasOwn(original.own, key)) return "scratch";
    return original.own[key] === service.own[key] ? "stored" : "edited";
  };

  return (
    <>
      <div className={styles.rows}>
        {stored.length === 0 && (
          <div className={styles.row}>
            <span className={styles.empty}>No variables stored on this service.</span>
          </div>
        )}
        {stored.map((k) => row(k, originOf(k)))}
      </div>
      <div className={styles.groupHead}>Injected by shed</div>
      <div className={styles.rows}>{provided.map((k) => row(k, "injected"))}</div>
    </>
  );
}
