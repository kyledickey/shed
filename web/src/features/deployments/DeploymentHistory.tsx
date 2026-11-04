import { useQuery } from "@tanstack/react-query";
import { deploymentPageQuery } from "../../api/deployments";
import { useCursorPages } from "../../api/paging";
import type { Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { Pager } from "../../components/Pager";
import { Pill } from "../../components/Pill";
import {
  RecordEnd,
  RecordId,
  RecordList,
  RecordMessage,
  RecordMeta,
  RecordRow,
} from "../../components/RecordList";
import { shortSha } from "../../lib/names";
import { relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import type { DeploymentActions } from "./actions";
import { deploymentTitle, triggerLabel } from "./CommitMeta";
import styles from "./DeploymentHistory.module.css";
import { DeploymentMenu } from "./DeploymentMenu";

const COLUMNS = "80px minmax(0, 1fr) 130px 120px minmax(0, 150px) 112px 32px";
const NARROW = "64px minmax(0, 1fr) auto 32px";

/** DeploymentHistory lists a service's deployments, newest first, a page at a time. */
export function DeploymentHistory({
  service,
  actions,
  onOpen,
}: {
  service: Service;
  actions: DeploymentActions;
  onOpen: (id: string) => void;
}) {
  const pages = useCursorPages();
  const { data, isPlaceholderData } = useQuery(deploymentPageQuery(service.id, pages.before));
  const deployments = data?.deployments ?? [];
  const next = data?.next ?? null;

  return (
    <RecordList
      title={
        <>
          <HelpTip topic="deploymentStatus">History</HelpTip>
        </>
      }
      footer={
        (pages.hasPrevious || next) && (
          <Pager
            count={deployments.length}
            hasPrevious={pages.hasPrevious}
            hasNext={next !== null}
            busy={isPlaceholderData}
            onPrevious={pages.previous}
            onNext={() => next && pages.next(next)}
          />
        )
      }
    >
      {deployments.map((d) => (
        <RecordRow
          key={d.id}
          columns={COLUMNS}
          narrowColumns={NARROW}
          onOpen={() => onOpen(d.id)}
          openLabel={`Open deployment ${deploymentTitle(d)}`}
        >
          <RecordId>{d.commitSha ? shortSha(d.commitSha) : "image"}</RecordId>
          <RecordMessage title={deploymentTitle(d)}>{deploymentTitle(d)}</RecordMessage>
          <span className={styles.status}>
            <StatusBadge kind="deployment" status={d.status} />
          </span>
          <RecordMeta optional>
            <Pill>{triggerLabel(d)}</Pill>
          </RecordMeta>
          <RecordMeta optional>{d.commitAuthor && `by ${d.commitAuthor}`}</RecordMeta>
          <RecordMeta optional className={styles.when}>
            {relativeTime(d.createdAt)}
          </RecordMeta>
          <RecordEnd>
            <DeploymentMenu deployment={d} actions={actions} onViewLogs={() => onOpen(d.id)} />
          </RecordEnd>
        </RecordRow>
      ))}
    </RecordList>
  );
}
