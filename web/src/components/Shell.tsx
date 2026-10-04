import { ChevronsUpDown } from "lucide-react";
import type { ReactNode } from "react";
import styles from "./Shell.module.css";

type ShellProps = {
  brand: ReactNode;
  nav: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
};

/**
 * Shell is the app frame: a rounded, bordered shell on the page canvas with
 * a top bar (brand, horizontal tabs, actions) and an inset content panel.
 */
export function Shell({ brand, nav, actions, children }: ShellProps) {
  return (
    <div className={styles.page}>
      <div className={styles.frame}>
        <header className={styles.bar}>
          <div className={styles.brand}>{brand}</div>
          <nav className={styles.nav}>{nav}</nav>
          {actions && <div className={styles.actions}>{actions}</div>}
        </header>
        <main className={styles.panel}>{children}</main>
      </div>
    </div>
  );
}

/** Logo is the shed mark: a little crayon house. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" className={styles.logo} aria-label="shed">
      <rect x="1" y="1" width="30" height="30" rx="9" className={styles.logoTile} />
      <path
        d="M8.5 15.2 16 9l7.5 6.2V23a1.5 1.5 0 0 1-1.5 1.5H10A1.5 1.5 0 0 1 8.5 23z"
        className={styles.logoHouse}
      />
      <rect x="13.5" y="17.5" width="5" height="7" rx="1.4" className={styles.logoDoor} />
    </svg>
  );
}

/** Crumbs is a project / service switcher trail for the shell bar. */
export function Crumbs({ items }: { items: { label: ReactNode; icon?: ReactNode }[] }) {
  return (
    <div className={styles.crumbs}>
      {items.map((item, i) => (
        <span key={i} className={styles.crumbGroup}>
          {i > 0 && <span className={styles.slash}>/</span>}
          <button type="button" className={styles.crumb}>
            {item.icon}
            <span>{item.label}</span>
            <ChevronsUpDown size={12} className={styles.crumbChevron} />
          </button>
        </span>
      ))}
    </div>
  );
}

/** PageHeader titles a page inside the panel. */
export function PageHeader({
  title,
  description,
  actions,
  eyebrow,
}: {
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  eyebrow?: ReactNode;
}) {
  return (
    <div className={styles.pageHeader}>
      <div>
        {eyebrow && <div className={styles.eyebrow}>{eyebrow}</div>}
        <h1 className={styles.pageTitle}>{title}</h1>
        {description && <p className={styles.pageDescription}>{description}</p>}
      </div>
      {actions && <div className={styles.pageActions}>{actions}</div>}
    </div>
  );
}
