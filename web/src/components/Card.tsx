import type { ReactNode } from "react";
import styles from "./Card.module.css";

type CardProps = {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  footer?: ReactNode;
  tone?: "default" | "danger";
  flush?: boolean;
  children?: ReactNode;
};

export function Card({
  title,
  description,
  actions,
  footer,
  tone = "default",
  flush,
  children,
}: CardProps) {
  return (
    <section className={styles.card} data-tone={tone}>
      {(title || actions) && (
        <header className={styles.header}>
          <div className={styles.heading}>
            {title && <h2 className={styles.title}>{title}</h2>}
            {description && <p className={styles.description}>{description}</p>}
          </div>
          {actions && <div className={styles.actions}>{actions}</div>}
        </header>
      )}
      {children && <div className={flush ? styles.flush : styles.body}>{children}</div>}
      {footer && <footer className={styles.footer}>{footer}</footer>}
    </section>
  );
}
