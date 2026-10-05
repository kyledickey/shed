import { useState, type ReactNode } from "react";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { Dialog } from "../../components/Overlay";
import styles from "./Settings.module.css";

type ConfirmDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  confirmLabel: string;
  /** Defaults to "danger". */
  confirmVariant?: "danger" | "primary";
  /** When set, the user must type this text before confirming. */
  typeToConfirm?: string;
  pending: boolean;
  error?: string;
  onConfirm: () => void;
  children: ReactNode;
};

/** ConfirmDialog asks before a destructive or disruptive action, optionally requiring a typed name. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  confirmLabel,
  confirmVariant = "danger",
  typeToConfirm,
  pending,
  error,
  onConfirm,
  children,
}: ConfirmDialogProps) {
  const [typed, setTyped] = useState("");
  const ready = !typeToConfirm || typed === typeToConfirm;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped("");
        onOpenChange(next);
      }}
      title={title}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button variant={confirmVariant} loading={pending} disabled={!ready} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className={styles.fields}>
        <div className={styles.note}>{children}</div>
        {typeToConfirm && (
          <Field label={`Type ${typeToConfirm} to confirm`}>
            {(id) => (
              <Input
                id={id}
                mono
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                autoComplete="off"
              />
            )}
          </Field>
        )}
        {error && <p className={styles.error}>{error}</p>}
      </div>
    </Dialog>
  );
}
