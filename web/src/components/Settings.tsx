import { useEffect, useState, type ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./Settings.module.css";

export type SettingsSection = {
  id: string;
  title: string;
  /** Buttons beside the section heading, such as "Add variable". */
  actions?: ReactNode;
  /** Marks the danger zone: its entry in the contents is red. */
  danger?: boolean;
  content: ReactNode;
};

/**
 * SettingsLayout stacks titled sections of setting rows, with a sticky table
 * of contents beside them that tracks the section in view.
 */
export function SettingsLayout({ sections }: { sections: SettingsSection[] }) {
  const [current, setCurrent] = useState(sections[0]?.id);
  useEffect(() => {
    const seen = new Map<string, boolean>();
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) seen.set(e.target.id, e.isIntersecting);
        const first = sections.find((s) => seen.get(s.id));
        if (first) setCurrent(first.id);
      },
      { rootMargin: "-80px 0px -55% 0px" },
    );
    for (const s of sections) {
      const el = document.getElementById(s.id);
      if (el) io.observe(el);
    }
    return () => io.disconnect();
  }, [sections]);

  return (
    <div className={styles.layout}>
      <div className={styles.sections}>
        {sections.map((s) => (
          <section key={s.id} id={s.id} className={styles.section}>
            <div className={styles.sectionHead}>
              <h2 className={styles.sectionTitle}>{s.title}</h2>
              {s.actions && <div className={styles.sectionActions}>{s.actions}</div>}
            </div>
            {s.content}
          </section>
        ))}
      </div>
      {sections.length > 1 && (
        <nav className={styles.toc} aria-label="On this page">
          {sections.map((s) => (
            <a
              key={s.id}
              href={`#${s.id}`}
              className={cx(styles.tocItem, s.danger && styles.tocDanger)}
              aria-current={s.id === current ? "location" : undefined}
            >
              {s.title}
            </a>
          ))}
        </nav>
      )}
    </div>
  );
}

/** Rows stacks setting rows 12px apart, for use outside a SettingsLayout. */
export function Rows({ children }: { children: ReactNode }) {
  return <div className={styles.rows}>{children}</div>;
}

type SettingRowProps = {
  label: ReactNode;
  description?: ReactNode;
  /** The control on the right: a switch, select, input, or button. */
  control?: ReactNode;
  /** Extra options or content under the label, inside a well. */
  children?: ReactNode;
  /** Put the control under the label at full width, for wide inputs. */
  stacked?: boolean;
};

/** SettingRow is one setting: a label on the left and its control on the right. */
export function SettingRow({ label, description, control, children, stacked }: SettingRowProps) {
  return (
    <div className={styles.row}>
      <div className={cx(styles.rowMain, stacked && styles.stacked)}>
        <div className={styles.rowText}>
          <span className={styles.rowLabel}>{label}</span>
          {description && <span className={styles.rowDescription}>{description}</span>}
        </div>
        {control && <div className={styles.rowControl}>{control}</div>}
      </div>
      {children}
    </div>
  );
}

/**
 * Well holds nested options inside a setting row, or says that a list is
 * empty. `action` sits on its right edge.
 */
export function Well({
  children,
  action,
  empty,
}: {
  children: ReactNode;
  action?: ReactNode;
  empty?: boolean;
}) {
  return (
    <div className={cx(styles.well, empty && styles.empty)}>
      <div className={styles.wellBody}>{children}</div>
      {action && <div className={styles.wellAction}>{action}</div>}
    </div>
  );
}
