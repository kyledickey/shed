import type { ReactNode } from "react";
import styles from "./Notice.module.css";

type NoticeProps = {
  /** A plain 16px glyph, or a spinner while something is in progress. */
  icon?: ReactNode;
  title: ReactNode;
  children?: ReactNode;
  /** Buttons on the right edge. */
  actions?: ReactNode;
  /** Shows the body in the danger color, for errors. */
  danger?: boolean;
};

/**
 * Notice is a quiet banner: a neutral panel with a title, an optional line
 * of detail, and actions on the right. Only an error's text takes color.
 */
export function Notice({ icon, title, children, actions, danger }: NoticeProps) {
  return (
    <div className={styles.notice} role={danger ? "alert" : "status"}>
      {icon && <span className={styles.icon}>{icon}</span>}
      <div className={styles.text}>
        <span className={styles.title}>{title}</span>
        {children && (
          <span className={styles.body} data-danger={danger || undefined}>
            {children}
          </span>
        )}
      </div>
      {actions && <div className={styles.actions}>{actions}</div>}
    </div>
  );
}
