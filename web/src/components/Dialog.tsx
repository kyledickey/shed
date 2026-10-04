import { Dialog as Base } from "@base-ui/react/dialog";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { buttonClass } from "./Button";
import styles from "./Dialog.module.css";

type DialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
  children: ReactNode;
};

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  size = "sm",
  children,
}: DialogProps) {
  return (
    <Base.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <Base.Portal>
        <Base.Backdrop className={styles.backdrop} />
        <Base.Popup className={`${styles.popup} ${styles[size]}`}>
          <header className={styles.header}>
            <div className={styles.heading}>
              <Base.Title className={styles.title}>{title}</Base.Title>
              {description && (
                <Base.Description className={styles.description}>{description}</Base.Description>
              )}
            </div>
            <Base.Close
              className={buttonClass({ variant: "ghost", size: "sm", icon: true })}
              aria-label="Close"
            >
              <X size={16} />
            </Base.Close>
          </header>
          {children}
        </Base.Popup>
      </Base.Portal>
    </Base.Root>
  );
}

/** A form spanning the dialog body and footer; submit is already prevented. */
export function DialogForm({ onSubmit, children }: { onSubmit: () => void; children: ReactNode }) {
  return (
    <form
      className={styles.form}
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      {children}
    </form>
  );
}

export function DialogBody({ children }: { children: ReactNode }) {
  return <div className={styles.body}>{children}</div>;
}

export function DialogFooter({ children }: { children: ReactNode }) {
  return <footer className={styles.footer}>{children}</footer>;
}
