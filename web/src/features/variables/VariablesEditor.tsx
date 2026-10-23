import { useQuery } from "@tanstack/react-query";
import { Check, Eye, EyeOff, Pencil, Plus, Trash2, KeyRound } from "lucide-react";
import { useRef, useState } from "react";
import { onePasswordVariablesQuery } from "../../api/onepassword";
import { resolveVariables, useSaveVariables } from "../../api/services";
import { errorMessage } from "../../api/client";
import type { Service, Variables } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Input, Segmented, Textarea } from "../../components/Form";
import { CopyButton, EmptyState, OnePasswordIcon } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { isValidKey, parseEnv, serializeEnv } from "../../lib/env";
import { useRedeployHint } from "../services/redeploy";
import { OnePasswordLink } from "./OnePasswordLink";
import { VarValue } from "./VarValue";
import styles from "./Variables.module.css";
import { HelpTip } from "../docs/HelpTip";

type Mode = "table" | "raw";
type Row = { id: number; key: string; value: string; editing: boolean };

/** diff counts keys added, removed, or changed between two variable sets. */
function diff(a: Variables, b: Variables): number {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  let n = 0;
  for (const k of keys) if (a[k] !== b[k]) n++;
  return n;
}

/** rowsToVars turns draft rows into variables, skipping blank rows and collecting errors. */
function rowsToVars(rows: Row[]): { vars: Variables; errors: string[] } {
  const vars: Variables = {};
  const errors: string[] = [];
  for (const { key, value } of rows) {
    const k = key.trim();
    if (!k && !value) continue;
    if (!isValidKey(k)) {
      errors.push(`"${k}" is not a valid name. Use letters, digits, and underscores.`);
    } else if (k in vars) {
      errors.push(`${k} is defined more than once.`);
    } else {
      vars[k] = value;
    }
  }
  return { vars, errors };
}

/**
 * VariablesEditor edits a service's variables as a table or raw .env text. The
 * table also lists, read-only, the variables of a linked 1Password Environment.
 */
export function VariablesEditor({
  service,
  variables,
}: {
  service: Service;
  variables: Variables;
}) {
  const serviceId = service.id;
  const link = service.onePassword;
  const [linking, setLinking] = useState(false);
  const save = useSaveVariables(serviceId);
  const hint = useRedeployHint();
  const toast = useToast();
  const nextId = useRef(0);
  const toRows = (vars: Variables): Row[] =>
    Object.entries(vars).map(([key, value]) => ({
      id: nextId.current++,
      key,
      value,
      editing: false,
    }));

  const [mode, setMode] = useState<Mode>("table");
  const [rows, setRows] = useState(() => toRows(variables));
  const [raw, setRaw] = useState("");
  const [revealed, setRevealed] = useState<ReadonlySet<number>>(new Set());

  const current = mode === "raw" ? parseEnv(raw) : rowsToVars(rows);
  const changes = diff(current.vars, variables);
  const dirty = changes > 0;
  const onePassword = useQuery({
    ...onePasswordVariablesQuery(serviceId, link?.environment ?? ""),
    enabled: link?.hasToken ?? false,
  });
  const fromOnePassword = onePassword.data ?? {};

  const update = (id: number, patch: Partial<Row>) =>
    setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...patch } : r)));

  const changeMode = (next: Mode) => {
    if (next === mode) return;
    if (next === "raw") {
      setRaw(serializeEnv(current.vars));
    } else {
      if (current.errors.length > 0) return;
      setRows(toRows(current.vars));
    }
    setMode(next);
  };

  const reset = (vars: Variables) => {
    setRows(toRows(vars));
    setRaw(serializeEnv(vars));
  };

  const onSave = () =>
    save.mutate(current.vars, {
      onSuccess: (saved) => {
        reset(saved);
        hint.markPending();
        toast.add({ type: "success", title: "Variables saved" });
      },
    });

  // copyValue copies the value a deployment would see. A saved value with
  // references is resolved by the server; an unsaved draft is copied as typed.
  const copyValue = (r: Row) => {
    if (!r.value.includes("${{") || variables[r.key] !== r.value) return r.value;
    return async () => {
      try {
        return (await resolveVariables(serviceId))[r.key] ?? "";
      } catch (err) {
        toast.add({
          type: "error",
          title: "Could not resolve variable",
          description: errorMessage(err),
        });
        throw err;
      }
    };
  };

  const toggleReveal = (id: number) =>
    setRevealed((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const footer = dirty && (
    <div className={styles.footer}>
      <span className={styles.footerText}>
        {changes} {changes === 1 ? "change" : "changes"}
      </span>
      <Button
        size="sm"
        disabled={save.isPending}
        onClick={() => {
          reset(variables);
          save.reset();
        }}
      >
        Discard
      </Button>
      <Button
        size="sm"
        variant="primary"
        loading={save.isPending}
        disabled={current.errors.length > 0}
        onClick={onSave}
      >
        Save
      </Button>
    </div>
  );

  return (
    <LayerCard
      title={
        <>
          Variables <HelpTip topic="variables" />
        </>
      }
      meta="References resolve at deploy time."
      actions={
        <>
          {mode === "table" && (
            <OnePasswordLink service={service} open={linking} onOpenChange={setLinking} />
          )}
          {mode === "table" && (rows.length > 0 || link) && (
            <Button
              size="sm"
              onClick={() =>
                setRows((rs) => [
                  ...rs,
                  { id: nextId.current++, key: "", value: "", editing: true },
                ])
              }
            >
              <Plus size={13} /> Add
            </Button>
          )}
          <Segmented
            size="sm"
            label="Editor mode"
            value={mode}
            onChange={changeMode}
            options={[
              { value: "table", label: "Table" },
              { value: "raw", label: "Raw" },
            ]}
          />
        </>
      }
      footer={footer || undefined}
    >
      {mode === "raw" ? (
        <div className={styles.raw}>
          <Textarea
            mono
            className={styles.rawText}
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            placeholder="KEY=value"
            spellCheck={false}
            aria-label="Variables in .env format"
          />
        </div>
      ) : rows.length === 0 && !link ? (
        <EmptyState
          icon={<KeyRound size={20} />}
          title="No variables yet"
          description="Variables are passed to the service as environment variables."
          actions={
            <Button
              size="sm"
              onClick={() => setRows([{ id: nextId.current++, key: "", value: "", editing: true }])}
            >
              <Plus size={13} /> Add variable
            </Button>
          }
        />
      ) : (
        <div className={styles.list}>
          {rows.map((r) =>
            r.editing ? (
              <div key={r.id} className={styles.row}>
                <Input
                  mono
                  className={styles.editKey}
                  value={r.key}
                  onChange={(e) => update(r.id, { key: e.target.value })}
                  placeholder="KEY"
                  aria-label="Variable name"
                  autoFocus={!r.key}
                  spellCheck={false}
                />
                <Input
                  mono
                  className={styles.editValue}
                  value={r.value}
                  onChange={(e) => update(r.id, { value: e.target.value })}
                  placeholder="value or ${{ service.KEY }}"
                  aria-label="Variable value"
                  spellCheck={false}
                />
                <span className={styles.actions}>
                  <Button
                    variant="ghost"
                    size="sm"
                    icon
                    aria-label="Done"
                    onClick={() => update(r.id, { editing: false })}
                  >
                    <Check size={14} />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    icon
                    aria-label={`Delete ${r.key || "variable"}`}
                    onClick={() => setRows((rs) => rs.filter((x) => x.id !== r.id))}
                  >
                    <Trash2 size={14} />
                  </Button>
                </span>
              </div>
            ) : (
              <div key={r.id} className={styles.row}>
                <span className={styles.key}>{r.key}</span>
                <span className={styles.value}>
                  {revealed.has(r.id) ? (
                    <VarValue value={r.value} />
                  ) : (
                    <span className={styles.masked}>••••••••</span>
                  )}
                </span>
                {r.key in fromOnePassword && (
                  <span className={styles.status}>overrides 1Password</span>
                )}
                <span className={styles.actions}>
                  <Button
                    variant="ghost"
                    size="sm"
                    icon
                    aria-label={revealed.has(r.id) ? "Hide" : "Reveal"}
                    onClick={() => toggleReveal(r.id)}
                  >
                    {revealed.has(r.id) ? <EyeOff size={14} /> : <Eye size={14} />}
                  </Button>
                  <CopyButton value={copyValue(r)} label={`Copy ${r.key}`} />
                  <Button
                    variant="ghost"
                    size="sm"
                    icon
                    aria-label={`Edit ${r.key}`}
                    onClick={() => update(r.id, { editing: true })}
                  >
                    <Pencil size={14} />
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    icon
                    aria-label={`Delete ${r.key}`}
                    onClick={() => setRows((rs) => rs.filter((x) => x.id !== r.id))}
                  >
                    <Trash2 size={14} />
                  </Button>
                </span>
              </div>
            ),
          )}
          {link && (
            <OnePasswordRows
              hasToken={link.hasToken}
              query={onePassword}
              shadowed={current.vars}
              onAddToken={() => setLinking(true)}
            />
          )}
        </div>
      )}
      {(current.errors.length > 0 || save.error) && (
        <ul className={styles.errors}>
          {current.errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
          {save.error && <li>{errorMessage(save.error)}</li>}
        </ul>
      )}
    </LayerCard>
  );
}

/** OnePasswordRows lists a linked Environment's variables, minus the ones the service overrides. */
function OnePasswordRows({
  hasToken,
  query,
  shadowed,
  onAddToken,
}: {
  hasToken: boolean;
  query: { data?: Variables; error: Error | null };
  shadowed: Variables;
  onAddToken: () => void;
}) {
  const [revealed, setRevealed] = useState<ReadonlySet<string>>(new Set());

  if (!hasToken) {
    return (
      <div className={styles.row}>
        <span className={styles.status}>Add a service account token to load this Environment.</span>
        <span className={styles.actions}>
          <Button size="sm" onClick={onAddToken}>
            Add token
          </Button>
        </span>
      </div>
    );
  }
  if (query.error) {
    return (
      <div className={styles.row}>
        <span className={styles.status} data-tone="danger">
          {errorMessage(query.error)}
        </span>
      </div>
    );
  }
  if (!query.data) {
    return (
      <div className={styles.row}>
        <span className={styles.status}>Loading from 1Password…</span>
      </div>
    );
  }

  if (Object.keys(query.data).length === 0) {
    return (
      <div className={styles.row}>
        <span className={styles.status}>This Environment has no variables.</span>
      </div>
    );
  }

  const toggle = (key: string) =>
    setRevealed((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  return Object.entries(query.data)
    .filter(([key]) => !(key in shadowed))
    .sort(([a], [b]) => (a < b ? -1 : 1))
    .map(([key, value]) => {
      const shown = revealed.has(key);
      return (
        <div key={key} className={styles.row}>
          <span className={styles.key}>
            <span className={styles.mark}>
              <OnePasswordIcon size={13} />
            </span>
            {key}
          </span>
          <span className={styles.value}>
            {shown ? value : <span className={styles.masked}>••••••••</span>}
          </span>
          <span className={styles.actions} data-readonly>
            <Button
              variant="ghost"
              size="sm"
              icon
              aria-label={shown ? "Hide" : "Reveal"}
              onClick={() => toggle(key)}
            >
              {shown ? <EyeOff size={14} /> : <Eye size={14} />}
            </Button>
            <CopyButton value={value} label={`Copy ${key}`} />
          </span>
        </div>
      );
    });
}
