import { DropdownMenu, Tabs } from "@cloudflare/kumo";
import { CaretUpDownIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { Fragment, type ReactNode } from "react";
import styles from "./PageHeader.module.css";

/** Crumb is one step of the trail above a page title; `menu` makes it a switcher. */
export type Crumb = { label: ReactNode; href?: string; menu?: ReactNode };

/** PageTab is a section of the page, linked so it can open in its own tab. */
export type PageTab = { value: string; label: ReactNode; href: string };

type PageHeaderProps = {
  crumbs?: Crumb[];
  icon?: ReactNode;
  title: ReactNode;
  /** Status and quiet facts beside or under the title. */
  meta?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  tabs?: PageTab[];
  tab?: string;
  /** Content between the title block and the tabs, such as a callout. */
  children?: ReactNode;
};

/**
 * PageHeader opens every page: a trail back up, the title with its actions,
 * and the page's sections as tabs along the bottom edge.
 */
export function PageHeader({
  crumbs,
  icon,
  title,
  meta,
  description,
  actions,
  tabs,
  tab,
  children,
}: PageHeaderProps) {
  const tabRow = tabs && (
    <Tabs
      variant="segmented"
      className={styles.tabs}
      value={tab}
      tabs={tabs.map((t) => ({
        value: t.value,
        label: t.label,
        nativeButton: false,
        render: <Link to={t.href} />,
      }))}
    />
  );
  const actionBox = actions && <div className={styles.actions}>{actions}</div>;
  return (
    <header className={styles.inner}>
      {crumbs && crumbs.length > 0 && <Trail crumbs={crumbs} />}
      <div className={styles.titleRow}>
        {icon && <span className={styles.icon}>{icon}</span>}
        <div className={styles.titleBlock}>
          <div className={styles.titleLine}>
            <h1 className={styles.title}>{title}</h1>
            {meta && <div className={styles.meta}>{meta}</div>}
          </div>
          {description && <p className={styles.description}>{description}</p>}
        </div>
        {!tabRow && actionBox}
      </div>
      {children}
      {tabRow && (
        <div className={styles.tabRow}>
          {tabRow}
          {actionBox}
        </div>
      )}
    </header>
  );
}

function Trail({ crumbs }: { crumbs: Crumb[] }) {
  return (
    <nav className={styles.trail} aria-label="Breadcrumb">
      {crumbs.map((c, i) => (
        <Fragment key={i}>
          {i > 0 && <span className={styles.slash}>/</span>}
          {c.menu ? (
            <DropdownMenu>
              <DropdownMenu.Trigger
                render={
                  <button type="button" className={styles.crumb}>
                    {c.label}
                    <CaretUpDownIcon size={12} className={styles.caret} />
                  </button>
                }
              />
              <DropdownMenu.Content align="start">{c.menu}</DropdownMenu.Content>
            </DropdownMenu>
          ) : c.href ? (
            <Link to={c.href} className={styles.crumb}>
              {c.label}
            </Link>
          ) : (
            <span className={styles.crumbCurrent}>{c.label}</span>
          )}
        </Fragment>
      ))}
    </nav>
  );
}
