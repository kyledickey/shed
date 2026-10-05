import { useState } from "react";
import { cx } from "../../../../lib/cx";
import { Demo } from "../../kit";
import styles from "./codebase.module.css";
import { importedBy, packages } from "./graph";

/** DepExplorer shows what a chosen package imports and who imports it. */
export function DepExplorer() {
  const [name, setName] = useState("deploy");
  const pkg = packages.find((p) => p.name === name) ?? { name, imports: [] };
  const users = importedBy(pkg.name);
  const leaf = pkg.imports.length === 0;

  return (
    <Demo title="Who imports whom">
      <div className={styles.chips} role="radiogroup" aria-label="Package">
        {packages.map((p) => (
          <button
            key={p.name}
            type="button"
            role="radio"
            aria-checked={p.name === name}
            className={cx(styles.chip, p.name === name && styles.chipOn)}
            onClick={() => setName(p.name)}
          >
            {p.name}
          </button>
        ))}
      </div>
      <dl className={styles.deps}>
        <dt>Imports</dt>
        <dd>
          <List items={pkg.imports} empty="Nothing from internal/. A leaf package." />
        </dd>
        <dt>Imported by</dt>
        <dd>
          <List items={["cmd/shed", ...users]} empty="" />
        </dd>
      </dl>
      <p className={styles.verdict}>
        {leaf
          ? `${pkg.name} can be read, tested, and replaced without touching any other package.`
          : `${pkg.name} declares small interfaces for what it needs, and cmd/shed passes in the real implementations.`}
      </p>
    </Demo>
  );
}

function List({ items, empty }: { items: string[]; empty: string }) {
  if (items.length === 0) return <span className={styles.none}>{empty}</span>;
  return (
    <span className={styles.names}>
      {items.map((i) => (
        <code key={i}>{i}</code>
      ))}
    </span>
  );
}
