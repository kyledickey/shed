import { useState } from "react";
import { Input, Segmented, Switch } from "../../../../components/Form";
import { archiveName, objectKey, planBackup, systemPlan } from "../../lib/backupPlan";
import { recoveryCommand, type Source } from "../../lib/recovery";
import { CodeBlock, Demo, DocTable } from "../../kit";
import styles from "../backups.module.css";

const kinds = ["postgres", "mysql", "mongo", "redis", "app", "shed.db"] as const;
type Kind = (typeof kinds)[number];

/** ArchiveDemo builds the archive name, S3 key, and recovery command for a typed setup. */
export function ArchiveDemo() {
  const [kind, setKind] = useState<Kind>("postgres");
  const [running, setRunning] = useState(true);
  const [encrypted, setEncrypted] = useState(true);
  const [prefix, setPrefix] = useState("shed");
  const [source, setSource] = useState<Source>("raw");

  const isSystem = kind === "shed.db";
  const plan = isSystem ? systemPlan : planBackup(kind === "app" ? "app" : kind, running);
  const file = archiveName("<id>", plan.ext, encrypted);
  const key = objectKey(prefix, isSystem ? null : "<serviceID>", file);
  const command = recoveryCommand({
    kind: isSystem ? "shed" : kind === "app" ? "app" : kind,
    ext: plan.ext,
    source,
    encrypted,
    rawFile: file,
    downloadName: archiveName("<name>-<timestamp>", plan.ext, false),
  });

  return (
    <Demo title="Archive, key, and recovery builder">
      <div className={styles.stack}>
        <div className={styles.controls}>
          <Segmented
            label="Service"
            value={kind}
            onChange={setKind}
            options={kinds.map((k) => ({ value: k, label: k }))}
          />
          <Switch label="running" checked={running} onChange={setRunning} />
          <Switch label="encryption on" checked={encrypted} onChange={setEncrypted} />
          <label className={styles.control}>
            S3 prefix
            <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} />
          </label>
        </div>
        <DocTable
          mono
          head={["Method", "Produced by", "Stored file", "S3 object key"]}
          rows={[[plan.method, plan.tool, file, key]]}
        />
        <Segmented
          label="Where the file came from"
          value={source}
          onChange={setSource}
          options={[
            { value: "raw", label: "Raw file" },
            { value: "download", label: "Dashboard download" },
          ]}
        />
        <CodeBlock lang="sh">{command}</CodeBlock>
        <p className={styles.summary}>
          {source === "download"
            ? "Downloads are decrypted by shed, so only zstd is needed."
            : encrypted
              ? "The stored file ends in .age and needs your age secret key."
              : "The archive is not encrypted, so only zstd is needed."}
        </p>
      </div>
    </Demo>
  );
}
