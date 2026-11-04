import { ArrowLeftIcon, ArrowRightIcon, CaretDownIcon, ListIcon } from "@phosphor-icons/react";
import { Link, Outlet, useLocation } from "@tanstack/react-router";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { cx } from "../../lib/cx";
import styles from "./DocsLayout.module.css";
import { docGroups, docPage, docPages, isDocSlug, type DocMeta } from "./registry";

/** currentSlug is the docs page in the URL, or undefined on /docs itself. */
function useCurrentSlug(): string | undefined {
  const { pathname } = useLocation();
  const slug = pathname.replace(/^\/docs\/?/, "").split("/")[0] ?? "";
  return isDocSlug(slug) ? slug : undefined;
}

/**
 * useScrollSpy returns the id of the section the reader is in: the last
 * section whose heading has scrolled into the top third of the window below
 * the top bar.
 * The document scrolls, so it listens to the window.
 */
function useScrollSpy(ids: readonly string[], offset: number): string | undefined {
  const [active, setActive] = useState<string | undefined>(ids[0]);
  const key = ids.join(" ");
  useEffect(() => {
    const list = key ? key.split(" ") : [];
    let frame = 0;
    const update = () => {
      frame = 0;
      const line = offset + Math.max(96, (window.innerHeight - offset) / 3);
      let current = list[0];
      for (const id of list) {
        const el = document.getElementById(id);
        if (el && el.getBoundingClientRect().top <= line) current = id;
      }
      // At the very bottom, the last section is the one being read.
      const doc = document.documentElement;
      if (
        list.length &&
        window.scrollY > 0 &&
        window.innerHeight + window.scrollY >= doc.scrollHeight - 4
      ) {
        current = list[list.length - 1];
      }
      setActive(current);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      cancelAnimationFrame(frame);
    };
  }, [key, offset]);
  return active;
}

/** PageTree lists every docs page by group, with the current page's sections under it. */
function PageTree({ slug, active }: { slug?: string; active?: string }) {
  return (
    <>
      {docGroups.map((group) => (
        <div key={group.title} className={styles.group}>
          <div className={styles.groupTitle}>{group.title}</div>
          {group.pages.map((p) => (
            <div key={p.slug} className={styles.pageItem}>
              <Link
                to="/docs/$slug"
                params={{ slug: p.slug }}
                className={cx(styles.pageLink, p.slug === slug && styles.current)}
                aria-current={p.slug === slug ? "page" : undefined}
              >
                {p.title}
              </Link>
              {p.slug === slug && p.sections.length > 0 && (
                <div className={styles.subTree}>
                  {p.sections.map((s) => (
                    <Link
                      key={s.id}
                      to="/docs/$slug"
                      params={{ slug: p.slug }}
                      hash={s.id}
                      activeOptions={{ includeHash: true }}
                      className={cx(styles.sectionLink, s.id === active && styles.on)}
                    >
                      {s.title}
                    </Link>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      ))}
    </>
  );
}

/** Contents is the sticky "On this page" list, with a bar on the section being read. */
function Contents({ page, active }: { page: DocMeta; active?: string }) {
  if (page.sections.length === 0) return null;
  return (
    <nav className={styles.toc} aria-label="On this page">
      <div className={styles.tocTitle}>On this page</div>
      <div className={styles.tocList}>
        {page.sections.map((s) => (
          <Link
            key={s.id}
            to="/docs/$slug"
            params={{ slug: page.slug }}
            hash={s.id}
            activeOptions={{ includeHash: true }}
            className={cx(styles.tocItem, s.id === active && styles.on)}
          >
            {s.title}
          </Link>
        ))}
      </div>
    </nav>
  );
}

/** Pager links the pages before and after this one. */
function Pager({ slug }: { slug: string }) {
  const index = docPages.findIndex((p) => p.slug === slug);
  const prev = index > 0 ? docPages[index - 1] : undefined;
  const next = index >= 0 ? docPages[index + 1] : undefined;
  if (!prev && !next) return null;
  return (
    <nav className={styles.pager} aria-label="Pages">
      {prev ? (
        <Link to="/docs/$slug" params={{ slug: prev.slug }} className={styles.pagerLink}>
          <span className={styles.pagerHint}>
            <ArrowLeftIcon size={14} />
            Previous
          </span>
          <span className={styles.pagerTitle}>{prev.title}</span>
        </Link>
      ) : (
        <span />
      )}
      {next && (
        <Link
          to="/docs/$slug"
          params={{ slug: next.slug }}
          className={cx(styles.pagerLink, styles.pagerNext)}
        >
          <span className={styles.pagerHint}>
            Next
            <ArrowRightIcon size={14} />
          </span>
          <span className={styles.pagerTitle}>{next.title}</span>
        </Link>
      )}
    </nav>
  );
}

/**
 * DocsLayout frames the docs: the page tree on the left, the page in the
 * middle, and the page's sections on the right. barHeight is the height of
 * the sticky top bar above it, which the sticky columns and heading jumps
 * clear.
 */
export function DocsLayout({ barHeight = 48 }: { barHeight?: number }) {
  const slug = useCurrentSlug();
  const { hash } = useLocation();
  const page = slug ? docPage(slug) : undefined;
  const active = useScrollSpy(page?.sections.map((s) => s.id) ?? [], barHeight);
  const [menuOpen, setMenuOpen] = useState(false);
  const tree = useRef<HTMLElement>(null);

  // A new page closes the narrow-screen menu and brings its tree entry into view.
  useEffect(() => {
    setMenuOpen(false);
    const nav = tree.current;
    const link = nav?.querySelector<HTMLElement>('[aria-current="page"]');
    if (!nav || !link) return;
    const top = link.getBoundingClientRect().top - nav.getBoundingClientRect().top + nav.scrollTop;
    if (top < nav.scrollTop || top + link.offsetHeight > nav.scrollTop + nav.clientHeight) {
      nav.scrollTop = top - nav.clientHeight / 3;
    }
  }, [slug]);

  // Sections in the URL need a jump once the page has rendered.
  useEffect(() => {
    if (hash) document.getElementById(hash)?.scrollIntoView({ block: "start" });
  }, [slug, hash]);

  const group = docGroups.find((g) => g.pages.some((p) => p.slug === slug));

  return (
    <div className={styles.layout} style={{ "--docs-top": `${barHeight}px` } as CSSProperties}>
      <div className={styles.menu}>
        <button
          type="button"
          className={styles.menuButton}
          aria-expanded={menuOpen}
          aria-controls="docs-menu"
          onClick={() => setMenuOpen((o) => !o)}
        >
          <ListIcon size={16} />
          <span className={styles.menuLabel}>
            {group && <span className={styles.menuGroup}>{group.title}</span>}
            <span className={styles.menuPage}>{page?.title ?? "Docs"}</span>
          </span>
          <CaretDownIcon size={14} className={cx(styles.caret, menuOpen && styles.open)} />
        </button>
        {menuOpen && (
          <nav id="docs-menu" className={styles.menuPanel} aria-label="Docs">
            <PageTree slug={slug} active={active} />
          </nav>
        )}
      </div>

      <nav ref={tree} className={styles.tree} aria-label="Docs">
        <PageTree slug={slug} active={active} />
      </nav>

      <div className={styles.main}>
        <Outlet />
        {slug && <Pager slug={slug} />}
      </div>

      <aside className={styles.aside}>{page && <Contents page={page} active={active} />}</aside>
    </div>
  );
}
