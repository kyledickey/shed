import { errorMessage } from "../../api/client";
import { useCancelDeployment, useRedeploy } from "../../api/deployments";
import { isPending, type Deployment, type Service } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { shortSha } from "../../lib/names";
import { useRedeployHint } from "../services/redeploy";
import { deploymentTitle } from "./CommitMeta";

/**
 * redeployLabel names the redeploy action for a deployment, or returns null
 * when it has no image to reuse. Superseded deployments are rollbacks.
 */
export function redeployLabel(d: Deployment): string | null {
  if (!d.image || isPending(d.status)) return null;
  return d.status === "removed" ? "Roll back to this" : "Redeploy";
}

/** useDeploymentActions wraps redeploy, cancel and copy with toasts. */
export function useDeploymentActions(service: Service) {
  const redeploy = useRedeploy(service.id);
  const cancel = useCancelDeployment(service.id);
  const hint = useRedeployHint();
  const toast = useToast();

  return {
    redeploy: (d: Deployment) =>
      redeploy.mutate(d.id, {
        onSuccess: () => {
          hint.clear();
          toast.add({
            title: d.status === "removed" ? "Rollback queued" : "Redeploy queued",
            description: deploymentTitle(d),
            type: "info",
          });
        },
        onError: (err) =>
          toast.add({ title: "Couldn't redeploy", description: errorMessage(err), type: "error" }),
      }),
    cancel: (d: Deployment) =>
      cancel.mutate(d.id, {
        onSuccess: () =>
          toast.add({
            title: "Deployment canceled",
            description: deploymentTitle(d),
            type: "info",
          }),
        onError: (err) =>
          toast.add({ title: "Couldn't cancel", description: errorMessage(err), type: "error" }),
      }),
    copySha: (d: Deployment) =>
      navigator.clipboard.writeText(d.commitSha).then(
        () => toast.add({ title: "Copied commit SHA", description: shortSha(d.commitSha) }),
        (err: unknown) =>
          toast.add({ title: "Couldn't copy", description: errorMessage(err), type: "error" }),
      ),
    isRedeploying: (d: Deployment) => redeploy.isPending && redeploy.variables === d.id,
    isCanceling: (d: Deployment) => cancel.isPending && cancel.variables === d.id,
  };
}

/** DeploymentActions is the value returned by useDeploymentActions. */
export type DeploymentActions = ReturnType<typeof useDeploymentActions>;
