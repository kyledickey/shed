import { Link, Outlet, useLocation, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, ArrowRight } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Select } from "../../components/Form";
import { cx } from "../../lib/cx";
import styles from "./DocsLayout.module.css";
import { docGroups, docPage, docPages, isDocSlug, type DocSlug } from "./registry";

/** currentSlug is the docs page in the URL, or undefined on /docs itself. */
function useCurrentSlug(): DocSlug | undefined {
  const { pathname } = useLocation();
  const slug = pathname.replace(/^\/docs\/?/, "").split("/")[0] ?? "";
  return isDocSlug(slug) ? slug : undefined;
}

/**
 * useScrollSpy returns the id of the section the reader is in: the last
 * section whose heading has scrolled past the top third of the scroller.
 */
function useScrollSpy(ids: readonly string[], root: HTMLElement | null): string | undefined {
  const [active, setActive] = useState<string | undefined>(ids[0]);
  useEffect(() => {
    const scroller = root?.closest("main");
    if (!scroller) return;
    const update = () => {
      const top = scroller.getBoundingClientRect().top + scroller.clientHeight / 3;
      let current = ids[0];
      for (const id of ids) {
        const el = document.getElementById(id);
        if (el && el.getBoundingClientRect().top <= top) current = id;
      }
      // At the very bottom, the last section is the one being read.
      if (scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 4) {
        current = ids[ids.length - 1];
      }
      setActive(current);
    };
    update();
    scroller.addEventListener("scroll", update, { passive: true });
    return () => scroller.removeEventListener("scroll", update);
  }, [ids, root]);
  return active;
}

/** DocsLayout frames the docs: page tree on the left, the page, and its outline on the right. */
export function DocsLayout() {
  const slug = useCurrentSlug();
  const { hash } = useLocation();
  const navigate = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const [root, setRoot] = useState<HTMLElement | null>(null);
  const page = slug ? docPage(slug) : undefined;
  const ids = page?.sections.map((s) => s.id) ?? [];
  const active = useScrollSpy(ids, root);

  useEffect(() => setRoot(ref.current), []);

  // The router resets the panel on navigation; sections in the URL need a jump.
  useEffect(() => {
    if (hash) document.getElementById(hash)?.scrollIntoView({ block: "start" });
  }, [slug, hash]);

  const index = slug ? docPages.findIndex((p) => p.slug === slug) : -1;
  const prev = index > 0 ? docPages[index - 1] : undefined;
  const next = index >= 0 ? docPages[index + 1] : undefined;

  return (
    <div ref={ref} className={styles.layout}>
      <div className={styles.picker}>
        <Select
          value={slug ?? ""}
          onChange={(next) =>
            isDocSlug(next) && void navigate({ to: "/docs/$slug", params: { slug: next } })
          }
          options={docGroups.flatMap((g) =>
            g.pages.map((p) => ({ value: p.slug, label: `${g.title} / ${p.title}` })),
          )}
        />
      </div>
      <nav className={styles.tree} aria-label="Docs">
        {docGroups.map((group) => (
          <div key={group.title} className={styles.group}>
            <div className={styles.groupTitle}>{group.title}</div>
            {group.pages.map((p) => (
              <div key={p.slug}>
                <Link
                  to="/docs/$slug"
                  params={{ slug: p.slug }}
                  className={cx(styles.pageLink, p.slug === slug && styles.current)}
                >
                  {p.title}
                </Link>
                {p.slug === slug && (
                  <div className={styles.subTree}>
                    {p.sections.map((s) => (
                      <Link
                        key={s.id}
                        to="/docs/$slug"
                        params={{ slug: p.slug }}
                        hash={s.id}
                        className={cx(styles.sectionLink, s.id === active && styles.current)}
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
      </nav>

      <div className={styles.main}>
        <Outlet />
        {(prev || next) && (
          <div className={styles.pager}>
            {prev ? (
              <Link to="/docs/$slug" params={{ slug: prev.slug }} className={styles.pagerLink}>
                <span className={styles.pagerHint}>
                  <ArrowLeft size={13} /> Previous
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
                  Next <ArrowRight size={13} />
                </span>
                <span className={styles.pagerTitle}>{next.title}</span>
              </Link>
            )}
          </div>
        )}
      </div>

      {page && (
        <aside className={styles.outline} aria-label="On this page">
          <div className={styles.groupTitle}>On this page</div>
          {page.sections.map((s) => (
            <Link
              key={s.id}
              to="/docs/$slug"
              params={{ slug: page.slug }}
              hash={s.id}
              className={cx(styles.outlineLink, s.id === active && styles.current)}
            >
              {s.title}
            </Link>
          ))}
        </aside>
      )}
    </div>
  );
}
