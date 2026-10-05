import { useState } from "react";
import { Input } from "../../../../components/Form";
import { formatBytes } from "../../../../lib/format";
import { Demo } from "../../kit";
import { diskGuard, guardVerdict } from "../../lib/builder";
import styles from "./builds.module.css";

const verdicts = {
  ok: "A build would start and keep running.",
  refuse: "A new build would be refused. One already running would continue.",
  cancel: "A running build would be canceled within 3 seconds.",
} as const;

/** DiskGuard shows what the guard does for the free space and build.min_free_mb the reader types. */
export function DiskGuard() {
  const [minFree, setMinFree] = useState("2048");
  const [free, setFree] = useState("1500");
  const guard = diskGuard(Math.max(0, Number.parseInt(minFree, 10) || 0));
  const freeBytes = Math.max(0, Number.parseInt(free, 10) || 0) * 1024 * 1024;

  return (
    <Demo title="What the guard does">
      <div className={styles.inputs}>
        <label className={styles.input}>
          <span className={styles.inputLabel}>build.min_free_mb</span>
          <Input
            mono
            type="number"
            min={0}
            value={minFree}
            onChange={(e) => setMinFree(e.target.value)}
          />
        </label>
        <label className={styles.input}>
          <span className={styles.inputLabel}>Free space (MiB)</span>
          <Input
            mono
            type="number"
            min={0}
            value={free}
            onChange={(e) => setFree(e.target.value)}
          />
        </label>
      </div>
      <p className={styles.note}>
        {guard.startBelow === 0
          ? "The guard is off."
          : `Refuse below ${formatBytes(guard.startBelow)}, cancel below ${formatBytes(guard.cancelBelow)}. ${verdicts[guardVerdict(freeBytes, guard)]}`}
      </p>
    </Demo>
  );
}
