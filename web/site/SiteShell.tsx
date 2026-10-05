import { Link, useLocation } from "@tanstack/react-router";
import { BookOpen, House, ScrollText } from "lucide-react";
import type { ReactNode } from "react";
import { buttonClass } from "../src/components/Button";
import { GitHubIcon } from "../src/components/Misc";
import { Logo, Shell } from "../src/components/Shell";
import { cx } from "../src/lib/cx";
import { repoUrl } from "./links";
import styles from "./SiteShell.module.css";

/**
 * SiteShell frames every page in the dashboard's own app frame: a shell bar
 * with the brand and page tabs, the page in the panel, and a status bar
 * underneath.
 */
export function SiteShell({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  const docs = pathname.startsWith("/docs");
  return (
    <Shell
      brand={
        <Link to="/" className={styles.brand}>
          <Logo size={26} />
          shed
        </Link>
      }
      nav={
        <div className={styles.tabs}>
          <Link to="/" className={cx(styles.tab, !docs && styles.current)}>
            <House size={15} />
            Home
          </Link>
          <Link
            to="/docs/$slug"
            params={{ slug: "overview" }}
            className={cx(styles.tab, docs && styles.current)}
          >
            <BookOpen size={15} />
            Docs
          </Link>
          <a href={`${repoUrl}/blob/main/CHANGELOG.md`} className={styles.tab}>
            <ScrollText size={15} />
            Changelog
          </a>
        </div>
      }
      actions={
        <a
          href={repoUrl}
          aria-label="shed on GitHub"
          className={buttonClass({ variant: "ghost", icon: true })}
        >
          <GitHubIcon size={16} />
        </a>
      }
      footer={
        <span className={styles.footer}>
          shed.land · free and open source by <a href="https://novmbr.org">November</a>
        </span>
      }
    >
      {children}
    </Shell>
  );
}
