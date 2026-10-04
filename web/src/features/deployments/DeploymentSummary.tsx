import { GitCommitHorizontal } from "lucide-react";
import type { Deployment, DeploymentTrigger } from "../../api/types";
import { shortSha } from "../../lib/names";
import styles from "./DeploymentSummary.module.css";

const triggers: Record<DeploymentTrigger, string> = {
  push: "Push",
  manual: "Manual",
  redeploy: "Redeploy",
  create: "Initial deploy",
};

export function DeploymentSummary({ deployment: d }: { deployment: Deployment }) {
  const title = d.commitMessage.split("\n")[0] || (d.image ? d.image : "Deployment");
  return (
    <div className={styles.summary}>
      <div className={styles.title} title={d.commitMessage || undefined}>
        {title}
      </div>
      <div className={styles.meta}>
        {d.commitSha && (
          <span className={styles.sha}>
            <GitCommitHorizontal size={12} />
            {shortSha(d.commitSha)}
          </span>
        )}
        {d.commitAuthor && <span>{d.commitAuthor}</span>}
        <span>{triggers[d.trigger]}</span>
      </div>
    </div>
  );
}
