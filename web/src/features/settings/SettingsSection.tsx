import { useState, type ReactNode } from "react";
import { useUpdateService } from "../../api/services";
import type { Service, ServicePatch } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { useRedeployHint } from "../services/redeploy";
import styles from "./SettingsSection.module.css";

/** Local draft of a subset of service fields, saved with one PATCH. */
export function useSectionForm<T extends ServicePatch>(service: Service, pick: (s: Service) => T) {
  const saved = pick(service);
  const [draft, setDraft] = useState(saved);
  const update = useUpdateService(service.id);
  const hint = useRedeployHint();
  const dirty = (Object.keys(saved) as (keyof T)[]).some((k) => saved[k] !== draft[k]);

  return {
    draft,
    dirty,
    pending: update.isPending,
    error: update.error,
    set: <K extends keyof T>(key: K, value: T[K]) => setDraft((d) => ({ ...d, [key]: value })),
    reset: () => setDraft(saved),
    save: () => update.mutate(draft, { onSuccess: hint.markPending }),
  };
}

type SectionForm = Pick<
  ReturnType<typeof useSectionForm>,
  "dirty" | "pending" | "error" | "reset" | "save"
>;

type SettingsSectionProps = {
  title: string;
  description?: ReactNode;
  form: SectionForm;
  children: ReactNode;
};

export function SettingsSection({ title, description, form, children }: SettingsSectionProps) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (form.dirty) form.save();
      }}
    >
      <Card
        title={title}
        description={description}
        footer={
          <>
            {form.error && <span className={styles.error}>{form.error.message}</span>}
            {form.dirty && (
              <Button onClick={form.reset} disabled={form.pending}>
                Reset
              </Button>
            )}
            <Button type="submit" variant="primary" disabled={!form.dirty} loading={form.pending}>
              Save
            </Button>
          </>
        }
      >
        {children}
      </Card>
    </form>
  );
}
