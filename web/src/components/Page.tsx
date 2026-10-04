import type { ReactNode } from "react";
import styles from "./Page.module.css";

type PageProps = {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
};

export function Page({ title, description, actions, children }: PageProps) {
  return (
    <main className={styles.page}>
      {title && (
        <header className={styles.header}>
          <div className={styles.heading}>
            <h1 className={styles.title}>{title}</h1>
            {description && <div className={styles.description}>{description}</div>}
          </div>
          {actions && <div className={styles.actions}>{actions}</div>}
        </header>
      )}
      {children}
    </main>
  );
}
