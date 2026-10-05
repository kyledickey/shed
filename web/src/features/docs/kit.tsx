import { SlidersHorizontal } from "lucide-react";
import type { ReactNode } from "react";
import { LayerCard } from "../../components/Card";
import { CopyButton } from "../../components/Misc";
import { cx } from "../../lib/cx";
import styles from "./kit.module.css";

/** CodeBlock shows code or terminal text with a copy button. */
export function CodeBlock({
  title,
  lang,
  children,
}: {
  /** File name or a short label, e.g. "shed.toml". */
  title?: string;
  /** Shown when there is no title, e.g. "sh". */
  lang?: string;
  children: string;
}) {
  const code = children.replace(/^\n/, "").replace(/\s+$/, "");
  return (
    <div className={styles.code}>
      <div className={styles.codeHead}>
        <span className={styles.codeTitle}>{title ?? lang}</span>
        <CopyButton value={code} label="Copy code" />
      </div>
      <pre className={styles.pre}>
        {code.split("\n").map((line, i) => (
          <span key={i} className={cx(styles.line, /^\s*(#|\/\/|--)/.test(line) && styles.comment)}>
            {line || " "}
            {"\n"}
          </span>
        ))}
      </pre>
    </div>
  );
}

/** DocTable is a compact reference table. */
export function DocTable({
  head,
  rows,
  mono,
}: {
  head: ReactNode[];
  rows: ReactNode[][];
  /** Render the first column in monospace. */
  mono?: boolean;
}) {
  return (
    <div className={styles.tableWrap}>
      <table className={cx(styles.table, mono && styles.monoFirst)}>
        <thead>
          <tr>
            {head.map((h, i) => (
              <th key={i}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              {r.map((c, j) => (
                <td key={j}>{c}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * Demo frames an interactive explainer that runs entirely in the browser on
 * sample data or what the reader types. It never calls the API.
 */
export function Demo({
  title,
  actions,
  padded = true,
  children,
}: {
  title: ReactNode;
  actions?: ReactNode;
  padded?: boolean;
  children: ReactNode;
}) {
  return (
    <LayerCard
      className={styles.demo}
      title={title}
      icon={<SlidersHorizontal size={14} />}
      actions={actions}
      padded={padded}
    >
      {children}
    </LayerCard>
  );
}

/** Figure frames a diagram with an optional caption. */
export function Figure({ caption, children }: { caption?: ReactNode; children: ReactNode }) {
  return (
    <figure className={styles.figure}>
      <div className={styles.figureBody}>{children}</div>
      {caption && <figcaption className={styles.caption}>{caption}</figcaption>}
    </figure>
  );
}

/** Term renders an identifier inline: a field, a status, a file name. */
export function Term({ children }: { children: ReactNode }) {
  return <code className={styles.term}>{children}</code>;
}
