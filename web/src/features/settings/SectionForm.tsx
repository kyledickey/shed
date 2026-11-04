import { Button as KumoButton } from "@cloudflare/kumo";
import { useState, type ReactNode } from "react";
import { errorMessage } from "../../api/client";
import { useUpdateService } from "../../api/services";
import type { Service, ServicePatch } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { useToast } from "../../components/Overlay";
import { Rows } from "../../components/Settings";
import { useRedeployHint } from "../services/redeploy";
import form from "./Form.module.css";
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

/** FormState is what a save bar needs to know about a draft. */
export type FormState = Pick<
  ReturnType<typeof useSectionForm>,
  "dirty" | "pending" | "error" | "reset" | "save"
> & {
  /** Disables Save while the draft can't be submitted. */
  invalid?: boolean;
  /** Label of the submit button. Defaults to "Save". */
  submitLabel?: string;
  /** Replaces "Unsaved changes" in the bar, e.g. a validation problem. */
  message?: ReactNode;
};

/**
 * SettingsForm wraps a group of setting rows that save together. While the
 * draft differs from what is saved, a slim bar under the rows offers Discard
 * and Save; Enter in any field saves too.
 */
export function SettingsForm({ form: state, children }: { form: FormState; children: ReactNode }) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (state.dirty && !state.invalid && !state.pending) state.save();
      }}
    >
      <Rows>
        {children}
        {state.dirty && <SaveBar form={state} />}
      </Rows>
    </form>
  );
}

function SaveBar({ form: state }: { form: FormState }) {
  return (
    <div className={form.saveBar} role="group" aria-label="Unsaved changes">
      <span className={state.error ? form.saveError : form.saveText} aria-live="polite">
        {state.error ? errorMessage(state.error) : (state.message ?? "Unsaved changes")}
      </span>
      <KumoButton size="sm" disabled={state.pending} onClick={state.reset}>
        Discard
      </KumoButton>
      <KumoButton
        type="submit"
        size="sm"
        variant="primary"
        disabled={state.invalid}
        loading={state.pending}
      >
        {state.submitLabel ?? "Save"}
      </KumoButton>
    </div>
  );
}

/**
 * SettingsCard is the older card form with its own Save button, kept for the
 * audit retention card until it moves to setting rows.
 */
export function SettingsCard({
  title,
  meta,
  form,
  footerStart,
  children,
}: {
  title: ReactNode;
  meta?: ReactNode;
  form?: FormState;
  /** Extra footer content on the left, e.g. a secondary action. */
  footerStart?: ReactNode;
  children: ReactNode;
}) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (form?.dirty && !form.invalid) form.save();
      }}
    >
      <LayerCard
        title={title}
        meta={meta}
        padded
        footer={
          <div className={styles.footer}>
            {footerStart}
            {form?.error ? (
              <span className={styles.error}>{errorMessage(form.error)}</span>
            ) : (
              <span className={styles.spacer} />
            )}
            {form?.dirty && (
              <Button size="sm" disabled={form.pending} onClick={form.reset}>
                Discard
              </Button>
            )}
            {form && (
              <Button
                type="submit"
                size="sm"
                variant="primary"
                disabled={!form.dirty || form.invalid}
                loading={form.pending}
              >
                {form.submitLabel ?? "Save"}
              </Button>
            )}
          </div>
        }
      >
        <div className={styles.fields}>{children}</div>
      </LayerCard>
    </form>
  );
}
