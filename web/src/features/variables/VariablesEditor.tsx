import { useState } from "react";
import { useSaveVariables } from "../../api/services";
import type { Variables } from "../../api/types";
import { Banner, ErrorText } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { Textarea } from "../../components/Input";
import { Segmented } from "../../components/Segmented";
import { parseEnv, serializeEnv } from "../../lib/env";
import { useRedeployHint } from "../services/redeploy";
import { VariableTable } from "./VariableTable";
import styles from "./VariablesEditor.module.css";

type Mode = "table" | "raw";

const modes: { value: Mode; label: string }[] = [
  { value: "table", label: "Table" },
  { value: "raw", label: "Raw editor" },
];

function sameVars(a: Variables, b: Variables): boolean {
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k]);
}

type VariablesEditorProps = { serviceId: string; variables: Variables };

export function VariablesEditor({ serviceId, variables }: VariablesEditorProps) {
  const save = useSaveVariables(serviceId);
  const hint = useRedeployHint();
  const [mode, setMode] = useState<Mode>("table");
  const [draft, setDraft] = useState(variables);
  const [raw, setRaw] = useState("");
  const [rawErrors, setRawErrors] = useState<string[]>([]);

  const current = mode === "raw" ? parseEnv(raw).vars : draft;
  const dirty = !sameVars(current, variables);

  const reset = (vars: Variables) => {
    setDraft(vars);
    setRaw(serializeEnv(vars));
    setRawErrors([]);
  };

  /** Applies the raw text to the draft; returns null if it doesn't parse. */
  const commitRaw = (): Variables | null => {
    const { vars, errors } = parseEnv(raw);
    setRawErrors(errors);
    if (errors.length > 0) return null;
    setDraft(vars);
    return vars;
  };

  const changeMode = (next: Mode) => {
    if (next === mode) return;
    if (next === "raw") {
      setRaw(serializeEnv(draft));
    } else if (!commitRaw()) {
      return;
    }
    setMode(next);
  };

  const onSave = () => {
    const vars = mode === "raw" ? commitRaw() : draft;
    if (!vars) return;
    save.mutate(vars, {
      onSuccess: (saved) => {
        reset(saved);
        hint.markPending();
      },
    });
  };

  return (
    <Card
      title="Variables"
      description={
        <>
          Reference other variables with <code>{"${{ KEY }}"}</code>, or another service's with{" "}
          <code>{"${{ service.KEY }}"}</code>.
        </>
      }
      actions={<Segmented label="Editor mode" value={mode} options={modes} onChange={changeMode} />}
      flush={mode === "table"}
      footer={
        <>
          {dirty && <span className={styles.unsaved}>Unsaved changes</span>}
          <Button disabled={!dirty || save.isPending} onClick={() => reset(variables)}>
            Discard
          </Button>
          <Button variant="primary" disabled={!dirty} loading={save.isPending} onClick={onSave}>
            Save variables
          </Button>
        </>
      }
    >
      {mode === "table" ? (
        <VariableTable variables={draft} onChange={setDraft} />
      ) : (
        <>
          <Textarea
            aria-label="Variables in .env format"
            value={raw}
            onChange={(e) => setRaw(e.target.value)}
            placeholder={"DATABASE_URL=${{ postgres.DATABASE_URL }}\nNODE_ENV=production"}
            rows={Math.max(12, raw.split("\n").length + 2)}
          />
          {rawErrors.length > 0 && <Banner tone="danger">{rawErrors.join(" · ")}</Banner>}
        </>
      )}
      {save.error && (
        <div className={mode === "table" ? styles.error : undefined}>
          <ErrorText error={save.error} />
        </div>
      )}
    </Card>
  );
}
