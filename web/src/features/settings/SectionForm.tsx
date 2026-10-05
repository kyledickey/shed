import type { ReactNode } from "react";
import { errorMessage } from "../../api/client";
import { useUpdateService } from "../../api/services";
import type { Service, ServicePatch } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { useToast } from "../../components/Overlay";
import { useRedeployHint } from "../services/redeploy";
import { useState } from "react";
import styles from "./Settings.module.css";

/** useSectionForm keeps a draft of some service fields and saves just those with one PATCH. */
export function useSectionForm<T extends ServicePatch>(service: Service, pick: (s: Service) => T) {
  const saved = pick(service);
  const [draft, setDraft] = useState(saved);
  const update = useUpdateService(service.id);
  const hint = useRedeployHint();
  const toast = useToast();
  const dirty = (Object.keys(saved) as (keyof T)[]).some((k) => saved[k] !== draft[k]);

  return {
    draft,
    dirty,
    pending: update.isPending,
    error: update.error,
    set: <K extends keyof T>(key: K, value: T[K]) => setDraft((d) => ({ ...d, [key]: value })),
    reset: () => {
      setDraft(saved);
      update.reset();
    },
    save: () =>
      update.mutate(draft, {
        onSuccess: () => {
          hint.markPending();
          toast.add({ type: "success", title: "Saved", description: "Redeploy to apply." });
        },
      }),
  };
}

type Form = Pick<
  ReturnType<typeof useSectionForm>,
  "dirty" | "pending" | "error" | "reset" | "save"
> & {
  /** Disables Save while the draft can't be submitted. */
  invalid?: boolean;
};

/** SettingsCard is a LayerCard wrapped in a form with its own Save button. */
export function SettingsCard({
  title,
  meta,
  form,
  footerStart,
  children,
}: {
  title: ReactNode;
  meta?: ReactNode;
  form: Form;
  /** Extra footer content on the left, e.g. a secondary action. */
  footerStart?: ReactNode;
  children: ReactNode;
}) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (form.dirty && !form.invalid) form.save();
      }}
    >
      <LayerCard
        title={title}
        meta={meta}
        padded
        footer={
          <div className={styles.footer}>
            {footerStart}
            {form.error ? (
              <span className={styles.error}>{errorMessage(form.error)}</span>
            ) : (
              <span className={styles.spacer} />
            )}
            {form.dirty && (
              <Button size="sm" disabled={form.pending} onClick={form.reset}>
                Discard
              </Button>
            )}
            <Button
              type="submit"
              size="sm"
              variant="primary"
              disabled={!form.dirty || form.invalid}
              loading={form.pending}
            >
              Save
            </Button>
          </div>
        }
      >
        <div className={styles.fields}>{children}</div>
      </LayerCard>
    </form>
  );
}
