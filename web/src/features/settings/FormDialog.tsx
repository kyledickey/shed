import { Dialog } from "@cloudflare/kumo";
import type { ReactNode } from "react";
import styles from "./Form.module.css";

type FormDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  /** Defaults to "lg" (512px). */
  size?: "base" | "lg" | "xl";
  children?: ReactNode;
  /** Buttons along the bottom, right-aligned. */
  footer?: ReactNode;
};

/** FormDialog is a Kumo dialog with a title, a body, and a footer of buttons. */
export function FormDialog({
  open,
  onOpenChange,
  title,
  description,
  size = "lg",
  children,
  footer,
}: FormDialogProps) {
  return (
    <Dialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <Dialog size={size}>
        <div className={styles.dialogBody}>
          <div className={styles.dialogHead}>
            <Dialog.Title className={styles.dialogTitle}>{title}</Dialog.Title>
            {description && (
              <Dialog.Description className={styles.dialogText} render={<div />}>
                {description}
              </Dialog.Description>
            )}
          </div>
          {children}
        </div>
        {footer && <div className={styles.dialogFoot}>{footer}</div>}
      </Dialog>
    </Dialog.Root>
  );
}
