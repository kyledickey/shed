import { useState, type ReactNode } from "react";
import { Button } from "./Button";
import { Dialog, DialogBody, DialogFooter, DialogForm } from "./Dialog";
import { Field } from "./Field";
import { Input } from "./Input";
import styles from "./ConfirmDialog.module.css";

type ConfirmDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  children: ReactNode;
  confirmLabel?: string;
  /** When set, the user must type this exact text to enable the confirm button. */
  typeToConfirm?: string;
  pending?: boolean;
  error?: string | null;
  onConfirm: () => void;
};

export function ConfirmDialog({ open, onOpenChange, title, ...rest }: ConfirmDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={title}>
      <ConfirmForm onCancel={() => onOpenChange(false)} {...rest} />
    </Dialog>
  );
}

function ConfirmForm({
  children,
  confirmLabel = "Delete",
  typeToConfirm,
  pending,
  error,
  onConfirm,
  onCancel,
}: Omit<ConfirmDialogProps, "open" | "onOpenChange" | "title"> & { onCancel: () => void }) {
  const [typed, setTyped] = useState("");
  const ready = !typeToConfirm || typed === typeToConfirm;
  return (
    <DialogForm onSubmit={() => ready && onConfirm()}>
      <DialogBody>
        <div className={styles.message}>{children}</div>
        {typeToConfirm && (
          <Field
            label={
              <>
                Type <code className={styles.code}>{typeToConfirm}</code> to confirm
              </>
            }
          >
            <Input mono value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus />
          </Field>
        )}
        {error && <p className={styles.error}>{error}</p>}
      </DialogBody>
      <DialogFooter>
        <Button onClick={onCancel}>Cancel</Button>
        <Button type="submit" variant="danger" disabled={!ready} loading={pending}>
          {confirmLabel}
        </Button>
      </DialogFooter>
    </DialogForm>
  );
}
