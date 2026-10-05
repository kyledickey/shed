import { useState } from "react";
import { Input } from "../../../../components/Form";
import { CodeBlock, Demo } from "../../kit";
import { builderCreateCommand } from "../../lib/builder";
import styles from "./builds.module.css";

/** BuilderPanel renders the builder-creation command shed runs for the limits the reader types. */
export function BuilderPanel() {
  const [memory, setMemory] = useState("2048");
  const [cpus, setCpus] = useState("2");
  const memoryMB = Math.max(0, Number.parseInt(memory, 10) || 0);
  const cores = Math.max(0, Number.parseFloat(cpus) || 0);

  return (
    <Demo title="Builder limits to command">
      <div className={styles.inputs}>
        <label className={styles.input}>
          <span className={styles.inputLabel}>build.memory_mb</span>
          <Input
            mono
            type="number"
            min={0}
            value={memory}
            onChange={(e) => setMemory(e.target.value)}
          />
        </label>
        <label className={styles.input}>
          <span className={styles.inputLabel}>build.cpus</span>
          <Input
            mono
            type="number"
            min={0}
            step={0.5}
            value={cpus}
            onChange={(e) => setCpus(e.target.value)}
          />
        </label>
      </div>
      <CodeBlock title="the commands shed runs, with these limits" lang="sh">
        {`docker buildx rm --keep-state shed
${builderCreateCommand(memoryMB, cores)}`}
      </CodeBlock>
    </Demo>
  );
}
