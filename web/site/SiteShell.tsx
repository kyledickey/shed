import { Link, useLocation } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { buttonClass } from "../src/components/Button";
import { GitHubIcon } from "../src/components/Misc";
import { Logo } from "../src/components/Shell";
import { cx } from "../src/lib/cx";
import { repoUrl } from "./links";
import styles from "./SiteShell.module.css";

/** SiteShell frames every page: the header, the page, and the footer. */
export function SiteShell({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  return (
    <div className={styles.site}>
      <header className={styles.header}>
        <div className={styles.bar}>
          <Link to="/" className={styles.brand}>
            <Logo size={26} />
            shed
          </Link>
          <nav className={styles.nav}>
            <Link
              to="/docs/$slug"
              params={{ slug: "overview" }}
              className={cx(styles.navLink, pathname.startsWith("/docs") && styles.current)}
            >
              Docs
            </Link>
            <a href={repoUrl} className={styles.navLink}>
              <GitHubIcon size={15} />
              GitHub
            </a>
          </nav>
          <Link
            to="/docs/$slug"
            params={{ slug: "installation" }}
            className={buttonClass({ variant: "primary", size: "sm" })}
          >
            Install
          </Link>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
      <footer className={styles.footer}>
        <div className={styles.footerBar}>
          <span className={styles.footerBrand}>
            <Logo size={20} />
            shed
          </span>
          <span className={styles.footerNote}>Self-hosted deploys for one server.</span>
          <nav className={styles.footerNav}>
            <Link to="/docs/$slug" params={{ slug: "overview" }}>
              Docs
            </Link>
            <a href={`${repoUrl}/blob/main/CHANGELOG.md`}>Changelog</a>
            <a href={repoUrl}>GitHub</a>
          </nav>
        </div>
      </footer>
    </div>
  );
}
