import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { Menu as BaseMenu } from "@base-ui/react/menu";
import { Toast } from "@base-ui/react/toast";
import { Tooltip as BaseTooltip } from "@base-ui/react/tooltip";
import { CircleCheck, CircleX, Info, X } from "lucide-react";
import type { ReactElement, ReactNode } from "react";
import { cx } from "../lib/cx";
import { buttonClass } from "./Button";
import styles from "./Overlay.module.css";
import type { Tone } from "./tone";

type DialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  iconTone?: Tone;
  size?: "sm" | "md" | "lg";
  children?: ReactNode;
  footer?: ReactNode;
};

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  icon,
  iconTone = "accent",
  size = "sm",
  children,
  footer,
}: DialogProps) {
  return (
    <BaseDialog.Root open={open} onOpenChange={(next) => onOpenChange(next)}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className={styles.backdrop} />
        <BaseDialog.Popup className={cx(styles.dialog, styles[size])}>
          <header className={styles.dialogHeader}>
            {icon && (
              <div className={styles.dialogIcon} data-tone={iconTone}>
                {icon}
              </div>
            )}
            <div className={styles.dialogHeading}>
              <BaseDialog.Title className={styles.dialogTitle}>{title}</BaseDialog.Title>
              {description && (
                <BaseDialog.Description className={styles.dialogDescription}>
                  {description}
                </BaseDialog.Description>
              )}
            </div>
            <BaseDialog.Close
              className={buttonClass({ variant: "ghost", size: "sm", icon: true })}
              aria-label="Close"
            >
              <X size={16} />
            </BaseDialog.Close>
          </header>
          {children && <div className={styles.dialogSheet}>{children}</div>}
          {footer && <footer className={styles.dialogFooter}>{footer}</footer>}
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}

type MenuProps = {
  trigger: ReactElement;
  align?: "start" | "center" | "end";
  children: ReactNode;
};

export function Menu({ trigger, align = "end", children }: MenuProps) {
  return (
    <BaseMenu.Root>
      <BaseMenu.Trigger render={trigger} />
      <BaseMenu.Portal>
        <BaseMenu.Positioner className={styles.positioner} sideOffset={6} align={align}>
          <BaseMenu.Popup className={styles.menu}>{children}</BaseMenu.Popup>
        </BaseMenu.Positioner>
      </BaseMenu.Portal>
    </BaseMenu.Root>
  );
}

type MenuItemProps = {
  onClick?: () => void;
  icon?: ReactNode;
  shortcut?: string;
  danger?: boolean;
  disabled?: boolean;
  /** Renders the item as a link, e.g. a file download. */
  href?: string;
  /** Makes the link download rather than navigate. Needs `href`. */
  download?: boolean;
  children: ReactNode;
};

export function MenuItem({
  onClick,
  icon,
  shortcut,
  danger,
  disabled,
  href,
  download,
  children,
}: MenuItemProps) {
  const content = (
    <>
      {icon && <span className={styles.menuIcon}>{icon}</span>}
      <span className={styles.menuLabel}>{children}</span>
      {shortcut && <kbd className={styles.menuShortcut}>{shortcut}</kbd>}
    </>
  );
  if (href) {
    return (
      <BaseMenu.LinkItem
        className={styles.menuItem}
        data-danger={danger || undefined}
        href={href}
        download={download}
        onClick={onClick}
      >
        {content}
      </BaseMenu.LinkItem>
    );
  }
  return (
    <BaseMenu.Item
      className={styles.menuItem}
      data-danger={danger || undefined}
      disabled={disabled}
      onClick={onClick}
    >
      {content}
    </BaseMenu.Item>
  );
}

export function MenuSeparator() {
  return <BaseMenu.Separator className={styles.menuSeparator} />;
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className={styles.menuGroupLabel}>{children}</div>;
}

/** Tooltip shows a short hint on hover or focus of its single child. */
export function Tooltip({
  content,
  children,
  side = "top",
}: {
  content: ReactNode;
  children: ReactElement;
  side?: "top" | "bottom" | "left" | "right";
}) {
  return (
    <BaseTooltip.Root>
      <BaseTooltip.Trigger render={children} />
      <BaseTooltip.Portal>
        <BaseTooltip.Positioner side={side} sideOffset={8} className={styles.positioner}>
          <BaseTooltip.Popup className={styles.tooltip}>{content}</BaseTooltip.Popup>
        </BaseTooltip.Positioner>
      </BaseTooltip.Portal>
    </BaseTooltip.Root>
  );
}

export const TooltipProvider = BaseTooltip.Provider;

export type ToastType = "success" | "error" | "info";

const toastIcons: Record<ToastType, ReactNode> = {
  success: <CircleCheck size={16} />,
  error: <CircleX size={16} />,
  info: <Info size={16} />,
};

const toastTones: Record<ToastType, string> = {
  success: "grass",
  error: "tomato",
  info: "sky",
};

/** ToastProvider renders the toast stack; call useToast().add() anywhere below it. */
export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <Toast.Provider>
      {children}
      <Toast.Portal>
        <Toast.Viewport className={styles.toastViewport}>
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}

function ToastList() {
  const { toasts } = Toast.useToastManager();
  return toasts.map((toast) => {
    const type = (toast.type as ToastType | undefined) ?? "info";
    return (
      <Toast.Root key={toast.id} toast={toast} className={styles.toast}>
        <Toast.Content className={styles.toastContent}>
          <span className={styles.toastIcon} data-tone={toastTones[type]}>
            {toastIcons[type]}
          </span>
          <div className={styles.toastText}>
            <Toast.Title className={styles.toastTitle} />
            <Toast.Description className={styles.toastDescription} />
          </div>
          <Toast.Close
            className={buttonClass({ variant: "ghost", size: "sm", icon: true })}
            aria-label="Dismiss"
          >
            <X size={14} />
          </Toast.Close>
        </Toast.Content>
      </Toast.Root>
    );
  });
}

export const useToast = Toast.useToastManager;
