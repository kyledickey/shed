import { Dialog as BaseDialog } from "@base-ui/react/dialog";
import { CornerDownLeft, Search } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import styles from "./CommandPalette.module.css";
import { Kbd } from "./Misc";

export type Command = {
  id: string;
  group: string;
  label: string;
  icon?: ReactNode;
  hint?: string;
  run: () => void;
};

/** CommandPalette is a ⌘K launcher over a flat list of commands. */
export function CommandPalette({
  open,
  onOpenChange,
  commands,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  commands: Command[];
}) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        onOpenChange(!open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onOpenChange]);

  const q = query.trim().toLowerCase();
  const matches = q ? commands.filter((c) => c.label.toLowerCase().includes(q)) : commands;
  const groups = [...new Set(matches.map((c) => c.group))];
  const ordered = groups.flatMap((g) => matches.filter((c) => c.group === g));
  const current = Math.min(active, ordered.length - 1);

  const run = (c: Command | undefined) => {
    if (!c) return;
    onOpenChange(false);
    c.run();
  };

  return (
    <BaseDialog.Root
      open={open}
      onOpenChange={(next) => {
        onOpenChange(next);
        if (!next) {
          setQuery("");
          setActive(0);
        }
      }}
    >
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className={styles.backdrop} />
        <BaseDialog.Popup className={styles.palette}>
          <BaseDialog.Title className={styles.srOnly}>Command palette</BaseDialog.Title>
          <label className={styles.search}>
            <Search size={16} />
            <input
              autoFocus
              placeholder="Search projects, services, actions…"
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={(e) => {
                if (e.key === "ArrowDown") {
                  e.preventDefault();
                  setActive((current + 1) % Math.max(1, ordered.length));
                } else if (e.key === "ArrowUp") {
                  e.preventDefault();
                  setActive((current - 1 + ordered.length) % Math.max(1, ordered.length));
                } else if (e.key === "Enter") {
                  run(ordered[current]);
                }
              }}
            />
            <Kbd>esc</Kbd>
          </label>
          <div className={styles.results}>
            {ordered.length === 0 && <div className={styles.empty}>Nothing matches “{query}”.</div>}
            {groups.map((g) => (
              <div key={g} className={styles.group}>
                <div className={styles.groupLabel}>{g}</div>
                {ordered
                  .map((c, i) => [c, i] as const)
                  .filter(([c]) => c.group === g)
                  .map(([c, i]) => (
                    <button
                      key={c.id}
                      type="button"
                      className={styles.item}
                      data-active={i === current || undefined}
                      onMouseMove={() => setActive(i)}
                      onClick={() => run(c)}
                    >
                      <span className={styles.itemIcon}>{c.icon}</span>
                      <span className={styles.itemLabel}>{c.label}</span>
                      {c.hint && <span className={styles.itemHint}>{c.hint}</span>}
                      {i === current && <CornerDownLeft size={13} className={styles.enter} />}
                    </button>
                  ))}
              </div>
            ))}
          </div>
          <footer className={styles.footer}>
            <span>
              <Kbd>↑</Kbd>
              <Kbd>↓</Kbd> navigate
            </span>
            <span>
              <Kbd>↵</Kbd> run
            </span>
          </footer>
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  );
}
