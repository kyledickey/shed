import { useState } from "react";
import type { DeploymentStatus } from "../../../../api/types";
import { StatusBadge } from "../../../../components/Badge";
import { Segmented, Select, Switch } from "../../../../components/Form";
import { Demo } from "../../kit";
import { cx } from "../../../../lib/cx";
import { deriveServiceStatus, statusRules } from "../../lib/serviceStatus";
import styles from "../concepts.module.css";

const latestOptions = [
  "none",
  "queued",
  "waiting",
  "building",
  "deploying",
  "active",
  "failed",
  "removed",
  "canceled",
  "skipped",
].map((value) => ({ value, label: value }));

type Active = "none" | "running" | "exited";

/** WhatIf lets the reader set the inputs of the status rules and see the result. */
export function WhatIf() {
  const [latest, setLatest] = useState("failed");
  const [stopped, setStopped] = useState(false);
  const [active, setActive] = useState<Active>("none");

  const { status, rule } = deriveServiceStatus({
    latest: latest === "none" ? null : (latest as DeploymentStatus),
    stopped,
    active: active === "none" ? null : { running: active === "running" },
  });

  return (
    <Demo title="Try the rules">
      <div className={styles.whatif}>
        <div className={styles.inputs}>
          <label className={styles.input}>
            <span className={styles.inputLabel}>Latest deployment</span>
            <Select value={latest} onChange={setLatest} options={latestOptions} mono />
          </label>
          <div className={styles.input}>
            <span className={styles.inputLabel}>Active deployment</span>
            <Segmented
              label="Active deployment"
              value={active}
              onChange={setActive}
              size="sm"
              options={[
                { value: "none", label: "None" },
                { value: "running", label: "Container up" },
                { value: "exited", label: "Container down" },
              ]}
            />
          </div>
          <Switch
            checked={stopped}
            onChange={setStopped}
            label="Stopped flag"
            description="Set by Stop, cleared by Start or a new deployment going live."
          />
        </div>
        <div className={styles.result}>
          <div className={styles.resultHead}>
            <span className={styles.inputLabel}>Status</span>
            <StatusBadge kind="service" status={status} />
          </div>
          <ol className={styles.rules}>
            {statusRules.map((r) => (
              <li key={r.id} className={cx(styles.rule, r.id === rule && styles.hit)}>
                {r.title}
              </li>
            ))}
          </ol>
        </div>
      </div>
    </Demo>
  );
}
