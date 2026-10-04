import type { Deployment, Service } from "../../api/types";
import { Count } from "../../components/Badge";
import { LayerCard } from "../../components/Card";
import { DeployRow } from "../../components/deploy/Deploy";
import { List } from "../../components/Layout";
import type { DeploymentActions } from "./actions";
import { deploymentTitle } from "./CommitMeta";
import styles from "./DeploymentHistory.module.css";
import { DeploymentMenu } from "./DeploymentMenu";

/** The API returns at most this many deployments. */
const HISTORY_LIMIT = 50;

/** DeploymentHistory lists a service's deployments, newest first. */
export function DeploymentHistory({
  service,
  deployments,
  actions,
  onOpen,
}: {
  service: Service;
  deployments: Deployment[];
  actions: DeploymentActions;
  onOpen: (id: string) => void;
}) {
  return (
    <LayerCard
      title="History"
      meta={<Count>{deployments.length}</Count>}
      sheetClassName={styles.sheet}
      footer={
        deployments.length >= HISTORY_LIMIT && (
          <span className={styles.foot}>Showing the {HISTORY_LIMIT} most recent deployments.</span>
        )
      }
    >
      <List>
        {deployments.map((d) => (
          <DeployRow
            key={d.id}
            deployment={{ ...d, commitMessage: deploymentTitle(d) }}
            branch={service.repo ? service.branch : undefined}
            current={d.status === "active" && service.status !== "stopped"}
            onClick={() => onOpen(d.id)}
            actions={
              <DeploymentMenu deployment={d} actions={actions} onViewLogs={() => onOpen(d.id)} />
            }
          />
        ))}
      </List>
    </LayerCard>
  );
}
