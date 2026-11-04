import { Button, Input } from "@cloudflare/kumo";
import { useState, type ReactNode } from "react";
import { FormDialog } from "./FormDialog";
import styles from "./Form.module.css";

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
    <FormDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped("");
        onOpenChange(next);
      }}
      size="base"
      title={title}
      description={children}
      footer={
        <>
          <Button onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant={confirmVariant === "danger" ? "destructive" : "primary"}
            loading={pending}
            disabled={!ready}
            onClick={onConfirm}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      {typeToConfirm && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (ready && !pending) onConfirm();
          }}
        >
          <Input
            label={
              <>
                Type <code className={styles.value}>{typeToConfirm}</code> to confirm
              </>
            }
            className={styles.mono}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </form>
      )}
      {error && <p className={styles.error}>{error}</p>}
    </FormDialog>
  );
}
