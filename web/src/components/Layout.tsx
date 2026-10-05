import type { CSSProperties, ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./Layout.module.css";

/** Page is the padded content column inside the shell panel. */
export function Page({ wide, children }: { wide?: boolean; children: ReactNode }) {
  return <div className={cx(styles.page, wide && styles.wide)}>{children}</div>;
}

/** Section groups a titled block of a page. */
export function Section({
  title,
  description,
  actions,
  children,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className={styles.section}>
      {(title || actions) && (
        <div className={styles.sectionHead}>
          <div>
            {title && <h2 className={styles.sectionTitle}>{title}</h2>}
            {description && <p className={styles.sectionDescription}>{description}</p>}
          </div>
          {actions && <div className={styles.sectionActions}>{actions}</div>}
        </div>
      )}
      {children}
    </section>
  );
}

/** Grid lays children out in as many columns of at least `min` pixels as fit. */
export function Grid({ min = 280, children }: { min?: number; children: ReactNode }) {
  return (
    <div className={styles.grid} style={{ "--min": `${min}px` } as CSSProperties}>
      {children}
    </div>
  );
}

/** Stack is a vertical flex column. */
export function Stack({ gap = 3, children }: { gap?: 2 | 3 | 4 | 6; children: ReactNode }) {
  return (
    <div className={styles.stack} style={{ gap: `var(--space-${gap})` }}>
      {children}
    </div>
  );
}

/** List is a bordered list of rows inside a LayerCard sheet. */
export function List({ children }: { children: ReactNode }) {
  return <div className={styles.list}>{children}</div>;
}

export function ListRow({ className, children }: { className?: string; children: ReactNode }) {
  return <div className={cx(styles.listRow, className)}>{children}</div>;
}
