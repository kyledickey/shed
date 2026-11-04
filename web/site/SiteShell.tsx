import { LinkButton } from "@cloudflare/kumo";
import { DesktopIcon, MoonIcon, SunIcon } from "@phosphor-icons/react";
import { Link, useLocation } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { GitHubIcon } from "../src/components/Misc";
import { Logo } from "../src/features/shell/Logo";
import { cx } from "../src/lib/cx";
import { useTheme, type Theme } from "../src/lib/theme";
import { repoUrl } from "./links";
import styles from "./SiteShell.module.css";

/**
 * SiteShell frames every page of the public site: a sticky top bar that
 * blurs the page under it, the page, and a footer. The document scrolls, so
 * back and forward swipes work.
 */
export function SiteShell({ children }: { children: ReactNode }) {
  const { pathname } = useLocation();
  return (
    <>
      <header className={styles.top}>
        <div className={styles.blur} aria-hidden>
          <span />
          <span />
          <span />
          <span />
        </div>
        <div className={styles.bar}>
          <Link to="/" className={styles.brand}>
            <Logo size={24} />
            shed
          </Link>
          <Link
            to="/changelog"
            className={styles.release}
            aria-label={`Latest release, ${__SHED_VERSION__}`}
          >
            {__SHED_VERSION__}
          </Link>
          <nav className={styles.nav} aria-label="Site">
            <Link
              to="/docs/$slug"
              params={{ slug: "overview" }}
              className={cx(styles.link, pathname.startsWith("/docs") && styles.current)}
            >
              Docs
            </Link>
            <Link
              to="/changelog"
              className={cx(styles.link, pathname.startsWith("/changelog") && styles.current)}
            >
              Changelog
            </Link>
          </nav>
          <div className={styles.actions}>
            <LinkButton
              variant="secondary"
              href={repoUrl}
              icon={<GitHubIcon size={15} />}
              aria-label="shed on GitHub"
            >
              <span className={styles.wide}>GitHub</span>
            </LinkButton>
            <LinkButton variant="primary" href="/#install">
              Install
            </LinkButton>
          </div>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
      <Footer />
    </>
  );
}

const columns: { title: string; links: { label: string; href: string }[] }[] = [
  {
    title: "Product",
    links: [
      { label: "Install", href: "/#install" },
      { label: "Documentation", href: "/docs/overview" },
      { label: "Changelog", href: "/changelog" },
    ],
  },
  {
    title: "Project",
    links: [
      { label: "Source code", href: repoUrl },
      { label: "Releases", href: `${repoUrl}/releases` },
      { label: "Issues", href: `${repoUrl}/issues` },
    ],
  },
];

function Footer() {
  return (
    <footer className={styles.footer}>
      <div className={styles.footerInner}>
        <div className={styles.about}>
          <Link to="/" className={styles.brand}>
            <Logo size={22} />
            shed
          </Link>
          <p className={styles.tagline}>Push-to-deploy on a server you own.</p>
          <Link to="/changelog" className={styles.latest}>
            Latest release <span className={styles.latestVersion}>{__SHED_VERSION__}</span>
          </Link>
        </div>
        {columns.map((c) => (
          <div key={c.title} className={styles.column}>
            <span className={styles.columnTitle}>{c.title}</span>
            {c.links.map((l) => (
              <a key={l.label} href={l.href}>
                {l.label}
              </a>
            ))}
          </div>
        ))}
      </div>
      <div className={styles.footerBase}>
        <span>
          Free and open source under Apache 2.0, by <a href="https://novmbr.org">November</a>.
        </span>
        <ThemeSwitch />
      </div>
    </footer>
  );
}

const themes: { value: Theme; label: string; Icon: typeof SunIcon }[] = [
  { value: "system", label: "System", Icon: DesktopIcon },
  { value: "light", label: "Light", Icon: SunIcon },
  { value: "dark", label: "Dark", Icon: MoonIcon },
];

function ThemeSwitch() {
  const [theme, setTheme] = useTheme();
  return (
    <div className={styles.theme} role="radiogroup" aria-label="Theme">
      {themes.map(({ value, label, Icon }) => (
        <button
          key={value}
          type="button"
          role="radio"
          aria-checked={theme === value}
          aria-label={label}
          title={label}
          className={styles.themeOption}
          onClick={() => setTheme(value)}
        >
          <Icon size={14} />
        </button>
      ))}
    </div>
  );
}
