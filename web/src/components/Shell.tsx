import { ChevronsUpDown } from "lucide-react";
import type { ReactNode } from "react";
import { Menu } from "./Overlay";
import styles from "./Shell.module.css";

type ShellProps = {
  brand: ReactNode;
  nav: ReactNode;
  actions?: ReactNode;
  /** A slim status bar under the panel. */
  footer?: ReactNode;
  children: ReactNode;
};

/**
 * Shell is the app frame: a rounded, bordered shell on the page canvas with
 * a top bar (brand, horizontal tabs, actions) and an inset content panel.
 */
export function Shell({ brand, nav, actions, footer, children }: ShellProps) {
  return (
    <div className={styles.page}>
      <div className={styles.frame}>
        <header className={styles.bar}>
          <div className={styles.brand}>{brand}</div>
          <nav className={styles.nav}>{nav}</nav>
          {actions && <div className={styles.actions}>{actions}</div>}
        </header>
        <main id="panel" className={styles.panel} data-scroll-restoration-id="panel">
          {children}
        </main>
        {footer && <footer className={styles.status}>{footer}</footer>}
      </div>
    </div>
  );
}

/** Logo is the shed mark: a barn-door shed on an accent tile. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" className={styles.logo} aria-label="shed">
      <rect x="1" y="1" width="30" height="30" rx="9" className={styles.logoTile} />
      <path d="M6.5 14.5 16 8.5l9.5 6v10h-19z" className={styles.logoShed} />
      <path d="M11.5 24.5v-8h9v8" className={styles.logoDoor} />
      <path d="M11.5 16.5l9 8m0-8-9 8" className={styles.logoBrace} />
    </svg>
  );
}

export type Crumb = {
  label: ReactNode;
  icon?: ReactNode;
  onClick?: () => void;
  /** Menu items; turns the crumb into a switcher. */
  menu?: ReactNode;
};

/** Crumbs is a project / service switcher trail for the shell bar. */
export function Crumbs({ items }: { items: Crumb[] }) {
  return (
    <div className={styles.crumbs}>
      {items.map((item, i) => (
        <span key={i} className={styles.crumbGroup}>
          {i > 0 && <span className={styles.slash}>/</span>}
          {item.menu ? (
            <Menu
              align="start"
              trigger={
                <button type="button" className={styles.crumb}>
                  {item.icon}
                  <span>{item.label}</span>
                  <ChevronsUpDown size={12} className={styles.crumbChevron} />
                </button>
              }
            >
              {item.menu}
            </Menu>
          ) : (
            <button type="button" className={styles.crumb} onClick={item.onClick}>
              {item.icon}
              <span>{item.label}</span>
            </button>
          )}
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
