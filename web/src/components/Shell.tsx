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

/** Logo is the shed mark: a gable shed sticker on an accent tile. */
export function Logo({ size = 28 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" className={styles.logo} aria-label="shed">
      <rect width="64" height="64" rx="16" className={styles.logoTile} />
      <g transform="matrix(1.08 0 0 1.08 -4.06 -4.06)">
        <path d="M19 34 35 20 51 34V51H19Z" className={styles.logoInk} />
        <path d="M16 31 32 17 48 31V48H16Z" className={styles.logoInk} />
        <path d="M16 31 32 17 48 31V48H16Z" className={styles.logoShed} />
        <path d="M27 36h10v12H27z" className={styles.logoDoor} />
      </g>
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
