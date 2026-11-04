import { Button, DropdownMenu } from "@cloudflare/kumo";
import {
  ArrowCounterClockwiseIcon,
  ArrowUUpLeftIcon,
  CopyIcon,
  DotsThreeIcon,
  ScrollIcon,
  XIcon,
} from "@phosphor-icons/react";
import { isPending, type Deployment } from "../../api/types";
import { imageGone, imageGoneLabel, redeployLabel, type DeploymentActions } from "./actions";

/** DeploymentMenu is the ⋯ menu on a deployment row. */
export function DeploymentMenu({
  deployment: d,
  actions,
  onViewLogs,
}: {
  deployment: Deployment;
  actions: DeploymentActions;
  onViewLogs: () => void;
}) {
  const redeploy = redeployLabel(d);
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            variant="ghost"
            size="sm"
            shape="square"
            icon={<DotsThreeIcon size={16} weight="bold" />}
            aria-label="Deployment actions"
          />
        }
      />
      <DropdownMenu.Content align="end">
        <DropdownMenu.Item icon={ScrollIcon} onClick={onViewLogs}>
          View logs
        </DropdownMenu.Item>
        {redeploy && (
          <DropdownMenu.Item
            icon={d.status === "removed" ? ArrowUUpLeftIcon : ArrowCounterClockwiseIcon}
            disabled={imageGone(d)}
            onClick={() => actions.redeploy(d)}
          >
            {imageGone(d) ? imageGoneLabel : redeploy}
          </DropdownMenu.Item>
        )}
        {d.commitSha && (
          <DropdownMenu.Item icon={CopyIcon} onClick={() => void actions.copySha(d)}>
            Copy commit SHA
          </DropdownMenu.Item>
        )}
        {isPending(d.status) && (
          <>
            <DropdownMenu.Separator />
            <DropdownMenu.Item icon={XIcon} variant="danger" onClick={() => actions.cancel(d)}>
              Cancel
            </DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
