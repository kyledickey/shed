import { Check, Eye, EyeOff, Pencil, Plus, Trash, X } from "lucide-react";
import { useState } from "react";
import type { Variables } from "../../api/types";
import { Button } from "../../components/Button";
import { CopyButton } from "../../components/CopyButton";
import { Input } from "../../components/Input";
import { isValidKey } from "../../lib/env";
import styles from "./VariableTable.module.css";

type VariableTableProps = {
  variables: Variables;
  onChange: (variables: Variables) => void;
};

export function VariableTable({ variables, onChange }: VariableTableProps) {
  const [editing, setEditing] = useState<string | null>(null);
  const [revealed, setRevealed] = useState<ReadonlySet<string>>(new Set());
  const entries = Object.entries(variables);

  const validate = (key: string, previous?: string): string | null => {
    if (!isValidKey(key))
      return "Keys use letters, digits, and underscores, and can't start with a digit.";
    if (key !== previous && key in variables) return `${key} already exists.`;
    return null;
  };

  const replace = (previous: string, key: string, value: string) => {
    onChange(Object.fromEntries(entries.map(([k, v]) => (k === previous ? [key, value] : [k, v]))));
    setEditing(null);
  };

  const remove = (key: string) => {
    onChange(Object.fromEntries(entries.filter(([k]) => k !== key)));
  };

  const toggle = (key: string) => {
    const next = new Set(revealed);
    if (!next.delete(key)) next.add(key);
    setRevealed(next);
  };

  return (
    <div className={styles.table}>
      {entries.length === 0 && (
        <p className={styles.empty}>
          No variables yet. Add one below or paste a .env file in the raw editor.
        </p>
      )}
      {entries.map(([key, value]) =>
        editing === key ? (
          <VariableForm
            key={key}
            initialKey={key}
            initialValue={value}
            validate={(k) => validate(k, key)}
            onSubmit={(k, v) => replace(key, k, v)}
            onCancel={() => setEditing(null)}
          />
        ) : (
          <div key={key} className={styles.row}>
            <span className={styles.key}>{key}</span>
            <button
              type="button"
              className={styles.value}
              onClick={() => toggle(key)}
              aria-label={revealed.has(key) ? `Hide ${key}` : `Reveal ${key}`}
            >
              {revealed.has(key) ? (
                <>
                  <span className={styles.revealed}>{value || <em>empty</em>}</span>
                  <EyeOff size={14} />
                </>
              ) : (
                <>
                  <span className={styles.masked}>••••••••••••</span>
                  <Eye size={14} />
                </>
              )}
            </button>
            <div className={styles.actions}>
              <CopyButton value={value} label={`Copy ${key}`} />
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={`Edit ${key}`}
                onClick={() => setEditing(key)}
              >
                <Pencil size={14} />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={`Delete ${key}`}
                onClick={() => remove(key)}
              >
                <Trash size={14} />
              </Button>
            </div>
          </div>
        ),
      )}
      <VariableForm
        key={entries.length}
        validate={(k) => validate(k)}
        onSubmit={(k, v) => onChange({ ...variables, [k]: v })}
      />
    </div>
  );
}

type VariableFormProps = {
  initialKey?: string;
  initialValue?: string;
  validate: (key: string) => string | null;
  onSubmit: (key: string, value: string) => void;
  onCancel?: () => void;
};

function VariableForm({
  initialKey = "",
  initialValue = "",
  validate,
  onSubmit,
  onCancel,
}: VariableFormProps) {
  const [key, setKey] = useState(initialKey);
  const [value, setValue] = useState(initialValue);
  const [error, setError] = useState<string | null>(null);
  const adding = !onCancel;

  return (
    <form
      className={styles.form}
      data-adding={adding || undefined}
      onSubmit={(e) => {
        e.preventDefault();
        const trimmed = key.trim();
        const problem = validate(trimmed);
        setError(problem);
        if (!problem) onSubmit(trimmed, value);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && onCancel) onCancel();
      }}
    >
      <div className={styles.row}>
        <Input
          mono
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="KEY"
          aria-label="Key"
          aria-invalid={error ? true : undefined}
          autoFocus={!adding}
        />
        <Input
          mono
          value={value}
          onChange={(e) => setValue(e.target.value)}
          placeholder="value"
          aria-label="Value"
        />
        <div className={styles.actions}>
          {adding ? (
            <Button type="submit" size="sm" disabled={!key.trim()}>
              <Plus size={14} />
              Add
            </Button>
          ) : (
            <>
              <Button type="submit" variant="ghost" size="sm" icon aria-label="Done">
                <Check size={14} />
              </Button>
              <Button variant="ghost" size="sm" icon aria-label="Cancel" onClick={onCancel}>
                <X size={14} />
              </Button>
            </>
          )}
        </div>
      </div>
      {error && <p className={styles.error}>{error}</p>}
    </form>
  );
}
