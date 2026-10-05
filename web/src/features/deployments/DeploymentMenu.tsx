import { Copy, Ellipsis, RotateCcw, ScrollText, Square, Undo2 } from "lucide-react";
import { isPending, type Deployment } from "../../api/types";
import { Button } from "../../components/Button";
import { Menu, MenuItem, MenuSeparator } from "../../components/Overlay";
import { imageGone, imageGoneLabel, redeployLabel, type DeploymentActions } from "./actions";

/** DeploymentMenu is the overflow menu on a deployment history row. */
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
    <Menu
      trigger={
        <Button variant="ghost" size="sm" icon aria-label="Deployment actions">
          <Ellipsis size={14} />
        </Button>
      }
    >
      <MenuItem icon={<ScrollText size={14} />} onClick={onViewLogs}>
        View logs
      </MenuItem>
      {redeploy && (
        <MenuItem
          icon={d.status === "removed" ? <Undo2 size={14} /> : <RotateCcw size={14} />}
          disabled={imageGone(d)}
          onClick={() => actions.redeploy(d)}
        >
          {imageGone(d) ? imageGoneLabel : redeploy}
        </MenuItem>
      )}
      {d.commitSha && (
        <MenuItem icon={<Copy size={14} />} onClick={() => void actions.copySha(d)}>
          Copy commit SHA
        </MenuItem>
      )}
      {isPending(d.status) && (
        <>
          <MenuSeparator />
          <MenuItem icon={<Square size={14} />} danger onClick={() => actions.cancel(d)}>
            Cancel
          </MenuItem>
        </>
      )}
    </Menu>
  );
}
