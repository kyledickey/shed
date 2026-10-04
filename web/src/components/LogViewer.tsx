import { ArrowDown } from "lucide-react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { LogLine } from "../api/events";
import { Button } from "./Button";
import styles from "./LogViewer.module.css";

type LogViewerProps = {
  lines: LogLine[];
  empty: ReactNode;
};

/** Monospace log output that follows new lines until the user scrolls up. */
export function LogViewer({ lines, empty }: LogViewerProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [follow, setFollow] = useState(true);

  useLayoutEffect(() => {
    const el = ref.current;
    if (follow && el) el.scrollTop = el.scrollHeight;
  }, [lines, follow]);

  return (
    <div className={styles.wrap}>
      <div
        ref={ref}
        className={styles.viewport}
        tabIndex={0}
        role="log"
        aria-live="off"
        onScroll={(e) => {
          const el = e.currentTarget;
          setFollow(el.scrollHeight - el.scrollTop - el.clientHeight < 24);
        }}
      >
        {lines.length === 0 ? (
          <div className={styles.empty}>{empty}</div>
        ) : (
          <pre className={styles.pre}>
            {lines.map((line) => (
              <div key={line.id} className={styles.line}>
                {line.text || " "}
              </div>
            ))}
          </pre>
        )}
      </div>
      {!follow && (
        <Button className={styles.jump} size="sm" onClick={() => setFollow(true)}>
          <ArrowDown size={14} />
          Follow
        </Button>
      )}
    </div>
  );
}
