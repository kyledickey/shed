import { useState } from "react";
import { Badge } from "../../../../components/Badge";
import { Input } from "../../../../components/Form";
import { configKeys, envToKey, keyToEnv, resolveEnv } from "../../lib/configKeys";
import { Demo, DocTable } from "../../kit";
import styles from "../configuration.module.css";

/** Explain describes what shed does with whatever the reader typed. */
function Explain({ text }: { text: string }) {
  const value = text.trim();
  if (!value) return <p className={styles.hint}>Type an environment variable or a TOML key.</p>;

  if (!value.startsWith("SHED_") && value.includes(".")) {
    const known = configKeys.find((k) => k.key === value);
    return (
      <div className={styles.result}>
        <span className={styles.arrow}>sets</span>
        <code className={styles.out}>{keyToEnv(value)}</code>
        {known ? (
          <Badge size="sm" tone="grass">
            known key, {known.type}, default {known.default}
          </Badge>
        ) : (
          <Badge size="sm" tone="tomato">
            not a shed key
          </Badge>
        )}
      </div>
    );
  }

  const r = resolveEnv(value);
  if (r.kind === "ignored") {
    return (
      <div className={styles.result}>
        <Badge size="sm" tone="tomato">
          ignored
        </Badge>
        <span>shed only reads variables that start with SHED_.</span>
      </div>
    );
  }
  if (r.kind === "unknown") {
    return (
      <div className={styles.result}>
        <span className={styles.arrow}>reads as</span>
        <code className={styles.out}>{r.key}</code>
        <Badge size="sm" tone="tomato">
          unknown key
        </Badge>
        <span>
          shed ignores it without a warning.
          {r.suggestion && (
            <>
              {" "}
              Did you mean <code>{keyToEnv(r.suggestion.key)}</code>?
            </>
          )}
        </span>
      </div>
    );
  }
  return (
    <div className={styles.result}>
      <span className={styles.arrow}>sets</span>
      <code className={styles.out}>{envToKey(value)}</code>
      <Badge size="sm" tone="grass">
        known key, {r.key.type}, default {r.key.default}
      </Badge>
    </div>
  );
}

/** EnvConverter converts between environment variable names and TOML keys. */
export function EnvConverter() {
  const [text, setText] = useState("SHED_PROXY_ACME_EMAIL");
  return (
    <>
      <Demo title="Name converter">
        <div className={styles.stack}>
          <Input
            mono
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="SHED_PROXY_ACME_EMAIL or proxy.acme_email"
            aria-label="Environment variable or TOML key"
            spellCheck={false}
          />
          <Explain text={text} />
        </div>
      </Demo>
      <DocTable
        mono
        head={["TOML key", "Environment variable", "Type", "Default"]}
        rows={configKeys.map((k) => [k.key, keyToEnv(k.key), k.type, k.default])}
      />
    </>
  );
}
