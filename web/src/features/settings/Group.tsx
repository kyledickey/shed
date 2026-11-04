import type { ReactNode } from "react";
import styles from "./Form.module.css";

/** Groups stacks the groups of a server section 40px apart. */
export function Groups({ children }: { children: ReactNode }) {
  return <div className={styles.groups}>{children}</div>;
}

/**
 * Group is one titled block of setting rows inside a server section, whose
 * own title the server page already shows.
 */
export function Group({
  title,
  description,
  actions,
  children,
}: {
  title?: ReactNode;
  description?: ReactNode;
  /** Buttons on the right of the title, such as "Add channel". */
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className={styles.group}>
      {(title || actions) && (
        <div className={styles.groupHead}>
          <div className={styles.groupText}>
            {title && <h3 className={styles.groupTitle}>{title}</h3>}
            {description && <p className={styles.groupDescription}>{description}</p>}
          </div>
          {actions && <div className={styles.groupActions}>{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}
