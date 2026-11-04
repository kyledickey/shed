import { Button } from "@cloudflare/kumo";
import { CheckIcon, CopyIcon } from "@phosphor-icons/react";
import { SlidersHorizontal } from "lucide-react";
import { useState, type ReactNode } from "react";
import { LayerCard } from "../../components/Card";
import { cx } from "../../lib/cx";
import styles from "./kit.module.css";

/** CopyCode copies text to the clipboard and shows a check for a moment. */
function CopyCode({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      variant="ghost"
      size="sm"
      shape="square"
      aria-label={copied ? "Copied" : "Copy code"}
      className={styles.copy}
      icon={copied ? <CheckIcon size={14} /> : <CopyIcon size={14} />}
      onClick={() => {
        navigator.clipboard.writeText(value).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          },
          () => {},
        );
      }}
    />
  );
}

/** CodeBlock shows code or terminal text on a recessed panel with a copy button. */
export function CodeBlock({
  title,
  lang,
  children,
}: {
  /** File name or a short label, e.g. "shed.toml". */
  title?: string;
  /** The fence's language, e.g. "sh". */
  lang?: string;
  children: string;
}) {
  const code = children.replace(/^\n/, "").replace(/\s+$/, "");
  return (
    <div className={cx(styles.code, title && styles.titled)} data-lang={lang}>
      {title && <div className={styles.codeHead}>{title}</div>}
      <pre className={styles.pre}>
        {code.split("\n").map((line, i) => (
          <span key={i} className={cx(/^\s*(#|\/\/|--)/.test(line) && styles.comment)}>
            {line || " "}
            {"\n"}
          </span>
        ))}
      </pre>
      <CopyCode value={code} />
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

/** CalloutTone is how much a callout matters: a note, a tip, a warning, or a danger. */
type CalloutTone = "note" | "tip" | "warning" | "danger";

/** Older crayon names that pages may still pass as a tone. */
const toneAliases: Record<string, CalloutTone | undefined> = {
  sky: "note",
  info: "note",
  grass: "tip",
  accent: "tip",
  sunflower: "warning",
  tangerine: "warning",
  tomato: "danger",
};

/**
 * Callout sets a paragraph apart: a quiet bordered block with a thin
 * colored bar on its left edge and an optional bold title.
 */
export function Callout({
  tone = "note",
  title,
  children,
}: {
  /** A CalloutTone, or a crayon name such as "sky" that maps to one. */
  tone?: string;
  title?: ReactNode;
  children?: ReactNode;
}) {
  const t = toneAliases[tone] ?? tone;
  return (
    <aside className={styles.callout} data-tone={t}>
      {title && <div className={styles.calloutTitle}>{title}</div>}
      {children && <div className={styles.calloutBody}>{children}</div>}
    </aside>
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
      sheetClassName={styles.demoSheet}
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
