import { useState } from "react";
import { Input } from "../../../../components/Form";
import { CodeBlock, Demo } from "../../kit";
import { isBuildKey, secretMounts } from "../../lib/buildPlan";
import styles from "../builds.module.css";

/** SecretSnippet writes the Dockerfile lines that read the variable names the reader types as build secrets. */
export function SecretSnippet() {
  const [names, setNames] = useState("DATABASE_URL, NPM_TOKEN");
  const [command, setCommand] = useState("npm run build");
  const keys = names
    .split(/[\s,]+/)
    .filter(Boolean)
    .filter((k, i, all) => all.indexOf(k) === i);
  const skipped = keys.filter((k) => !isBuildKey(k));

  return (
    <Demo title="Mount variables as build secrets">
      <div className={styles.inputs}>
        <label className={styles.input}>
          <span className={styles.inputLabel}>Variable names</span>
          <Input mono value={names} onChange={(e) => setNames(e.target.value)} />
        </label>
        <label className={styles.input}>
          <span className={styles.inputLabel}>Build command</span>
          <Input mono value={command} onChange={(e) => setCommand(e.target.value)} />
        </label>
      </div>
      {skipped.length > 0 && (
        <p className={styles.warn}>
          Not valid secret ids, so shed leaves them out: {skipped.join(", ")}
        </p>
      )}
      <CodeBlock title="Dockerfile" lang="dockerfile">
        {secretMounts(keys, command)}
      </CodeBlock>
    </Demo>
  );
}
