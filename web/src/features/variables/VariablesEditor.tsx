import { Button, InputArea, Input, LayerCard, Table, Tabs } from "@cloudflare/kumo";
import {
  CheckIcon,
  CopyIcon,
  EyeIcon,
  EyeSlashIcon,
  PencilSimpleIcon,
  PlusIcon,
  TrashIcon,
  WarningIcon,
  XCircleIcon,
} from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useRef, useState, type ReactNode } from "react";
import { errorMessage, isApiError } from "../../api/client";
import { onePasswordQuery } from "../../api/onepassword";
import { resolveVariables, useSaveVariables } from "../../api/services";
import type { Variables } from "../../api/types";
import { OnePasswordIcon } from "../../components/Misc";
import { Notice } from "../../components/Notice";
import { useToast } from "../../components/Overlay";
import { RecordEmpty } from "../../components/RecordList";
import { isValidKey, parseEnv, serializeEnv } from "../../lib/env";
import { HelpTip } from "../docs/HelpTip";
import { useRedeployHint } from "../services/redeploy";
import styles from "./Variables.module.css";
import { VarValue } from "./VarValue";

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

/** Meta says where the variables are stored and when references resolve. */
function Meta() {
  const { data } = useSuspenseQuery(onePasswordQuery);
  return (
    <span className={styles.meta}>
      {data.connected && (
        <>
          <OnePasswordIcon size={14} />
          Stored in 1Password.
        </>
      )}{" "}
      References resolve at deploy time.
    </span>
  );
}

/** VarsCard is the card around the variables: a title strip with actions over a panel. */
function VarsCard({
  actions,
  footer,
  children,
}: {
  actions?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
}) {
  return (
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.title}>
          <HelpTip topic="variables">Variables</HelpTip>
        </span>
        <Meta />
        {actions && <div className={styles.headActions}>{actions}</div>}
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        {children}
        {footer}
      </LayerCard.Primary>
    </LayerCard>
  );
}

/**
 * VariablesUnavailable stands in for the editor when the variables can't be
 * read from 1Password. A missing item is replaced by saving an empty set.
 */
export function VariablesUnavailable({ serviceId, error }: { serviceId: string; error: Error }) {
  const save = useSaveVariables(serviceId);
  const toast = useToast();
  return (
    <VarsCard>
      <div className={styles.pad}>
        {isApiError(error, 409) ? (
          <Notice
            icon={<WarningIcon size={16} />}
            title="This service's 1Password item is missing"
            actions={
              <Button
                size="sm"
                loading={save.isPending}
                onClick={() =>
                  save.mutate(
                    {},
                    { onSuccess: () => toast.add({ type: "success", title: "Item created" }) },
                  )
                }
              >
                Create item
              </Button>
            }
          >
            It was deleted or moved in 1Password. Saving variables creates a new, empty item.
            {save.error && <span className={styles.error}> {errorMessage(save.error)}</span>}
          </Notice>
        ) : (
          <Notice icon={<XCircleIcon size={16} />} title="Could not load variables" danger>
            {errorMessage(error)}
          </Notice>
        )}
      </div>
    </VarsCard>
  );
}

function copy(value: string | (() => Promise<string>)): Promise<void> {
  if (typeof value === "string") return navigator.clipboard.writeText(value);
  const blob = value().then((text) => new Blob([text], { type: "text/plain" }));
  return navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]);
}

/** CopyValue copies a value, or the result of calling it, and shows a check once copied. */
function CopyValue({ value, label }: { value: string | (() => Promise<string>); label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      variant="ghost"
      size="sm"
      shape="square"
      aria-label={label}
      icon={copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
      onClick={() =>
        copy(value).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          },
          () => {},
        )
      }
    />
  );
}

/** VariablesEditor edits a service's variables as a table or raw .env text. */
export function VariablesEditor({
  serviceId,
  variables,
}: {
  serviceId: string;
  variables: Variables;
}) {
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

  const update = (id: number, patch: Partial<Row>) =>
    setRows((rs) => rs.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  const remove = (id: number) => setRows((rs) => rs.filter((x) => x.id !== id));
  const add = () =>
    setRows((rs) => [...rs, { id: nextId.current++, key: "", value: "", editing: true }]);

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

  const footer = (dirty || current.errors.length > 0 || save.error) && (
    <div className={styles.footer}>
      {(current.errors.length > 0 || save.error) && (
        <ul className={styles.errors}>
          {current.errors.map((e) => (
            <li key={e}>{e}</li>
          ))}
          {save.error && <li>{errorMessage(save.error)}</li>}
        </ul>
      )}
      {dirty && (
        <div className={styles.saveBar}>
          <span className={styles.changes}>
            {changes} unsaved {changes === 1 ? "change" : "changes"}
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
      )}
    </div>
  );

  return (
    <VarsCard
      actions={
        <>
          {mode === "table" && rows.length > 0 && (
            <Button size="sm" icon={<PlusIcon size={14} />} onClick={add}>
              Add
            </Button>
          )}
          <Tabs
            variant="segmented"
            size="sm"
            value={mode}
            onValueChange={(v) => changeMode(v as Mode)}
            tabs={[
              { value: "table", label: "Table" },
              { value: "raw", label: "Raw" },
            ]}
          />
        </>
      }
      footer={footer || undefined}
    >
      {mode === "raw" ? (
        <div className={styles.pad}>
          <InputArea
            className={styles.rawText}
            value={raw}
            onValueChange={setRaw}
            placeholder="KEY=value"
            spellCheck={false}
            aria-label="Variables in .env format"
          />
          <p className={styles.hint}>
            One <code>KEY=value</code> per line. Paste a whole .env file to add many at once.
          </p>
        </div>
      ) : rows.length === 0 ? (
        <RecordEmpty
          action={
            <Button size="sm" icon={<PlusIcon size={14} />} onClick={add}>
              Add variable
            </Button>
          }
        >
          No variables yet. They are passed to the service as environment variables.
        </RecordEmpty>
      ) : (
        <Table className={styles.table}>
          <Table.Header variant="compact">
            <Table.Row>
              <Table.Head className={styles.keyCol}>Name</Table.Head>
              <Table.Head>Value</Table.Head>
              <Table.Head className={styles.actionsCol}>
                <span className={styles.srOnly}>Actions</span>
              </Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {rows.map((r) =>
              r.editing ? (
                <Table.Row key={r.id}>
                  <Table.Cell>
                    <Input
                      size="sm"
                      className={styles.input}
                      value={r.key}
                      onChange={(e) => update(r.id, { key: e.target.value })}
                      placeholder="KEY"
                      aria-label="Variable name"
                      autoFocus={!r.key}
                      spellCheck={false}
                    />
                  </Table.Cell>
                  <Table.Cell>
                    <Input
                      size="sm"
                      className={styles.input}
                      value={r.value}
                      onChange={(e) => update(r.id, { value: e.target.value })}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") update(r.id, { editing: false });
                      }}
                      placeholder="value or ${{ service.KEY }}"
                      aria-label="Variable value"
                      spellCheck={false}
                    />
                  </Table.Cell>
                  <Table.Cell>
                    <span className={styles.actions}>
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label="Done"
                        icon={<CheckIcon size={14} />}
                        onClick={() => update(r.id, { editing: false })}
                      />
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label={`Delete ${r.key || "variable"}`}
                        icon={<TrashIcon size={14} />}
                        onClick={() => remove(r.id)}
                      />
                    </span>
                  </Table.Cell>
                </Table.Row>
              ) : (
                <Table.Row key={r.id}>
                  <Table.Cell className={styles.key}>{r.key}</Table.Cell>
                  <Table.Cell className={styles.value}>
                    {revealed.has(r.id) ? (
                      <VarValue value={r.value} />
                    ) : (
                      <span className={styles.masked} aria-label="Hidden value">
                        ••••••••
                      </span>
                    )}
                  </Table.Cell>
                  <Table.Cell>
                    <span className={styles.actions}>
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label={revealed.has(r.id) ? `Hide ${r.key}` : `Reveal ${r.key}`}
                        icon={
                          revealed.has(r.id) ? <EyeSlashIcon size={14} /> : <EyeIcon size={14} />
                        }
                        onClick={() => toggleReveal(r.id)}
                      />
                      <CopyValue value={copyValue(r)} label={`Copy ${r.key}`} />
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label={`Edit ${r.key}`}
                        icon={<PencilSimpleIcon size={14} />}
                        onClick={() => update(r.id, { editing: true })}
                      />
                      <Button
                        variant="ghost"
                        size="sm"
                        shape="square"
                        aria-label={`Delete ${r.key}`}
                        icon={<TrashIcon size={14} />}
                        onClick={() => remove(r.id)}
                      />
                    </span>
                  </Table.Cell>
                </Table.Row>
              ),
            )}
          </Table.Body>
        </Table>
      )}
    </VarsCard>
  );
}
