import { Menu as Base } from "@base-ui/react/menu";
import type { ReactElement, ReactNode } from "react";
import styles from "./Menu.module.css";

type MenuProps = {
  trigger: ReactElement;
  align?: "start" | "center" | "end";
  children: ReactNode;
};

export function Menu({ trigger, align = "end", children }: MenuProps) {
  return (
    <Base.Root>
      <Base.Trigger render={trigger} />
      <Base.Portal>
        <Base.Positioner className={styles.positioner} sideOffset={6} align={align}>
          <Base.Popup className={styles.popup}>{children}</Base.Popup>
        </Base.Positioner>
      </Base.Portal>
    </Base.Root>
  );
}

type MenuItemProps = {
  onClick: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
  children: ReactNode;
};

export function MenuItem({ onClick, icon, danger, disabled, children }: MenuItemProps) {
  return (
    <Base.Item
      className={styles.item}
      data-danger={danger || undefined}
      disabled={disabled}
      onClick={onClick}
    >
      {icon && <span className={styles.icon}>{icon}</span>}
      {children}
    </Base.Item>
  );
}

export function MenuSeparator() {
  return <Base.Separator className={styles.separator} />;
}

export function MenuLabel({ children }: { children: ReactNode }) {
  return <div className={styles.label}>{children}</div>;
}
