import { Button } from "@cloudflare/kumo";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useCancelDeployment, useDeploy, useRedeploy } from "../../api/deployments";
import { useRestartService, useStartService, useStopService } from "../../api/services";
import { isPending, type Service } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { useRedeployHint } from "./redeploy";

type Run = (
  mutate: (opts: { onSuccess: () => void; onError: (err: Error) => void }) => void,
  done: string,
  failed: string,
  after?: () => void,
) => void;

/**
 * ServiceControls are the run actions for the service's current state:
 * Restart and Stop while running, Start and Redeploy while stopped, Cancel
 * while a deployment is in progress, and Deploy when nothing is running.
 * A service fenced by a restore gets none: it can't run until the fence goes.
 */
export function ServiceControls({ service }: { service: Service }) {
  const toast = useToast();
  const hint = useRedeployHint();
  const deploy = useDeploy(service.id);
  const redeploy = useRedeploy(service.id);
  const cancel = useCancelDeployment(service.id);
  const stop = useStopService(service.id);
  const start = useStartService(service.id);
  const restart = useRestartService(service.id);
  const [confirmStop, setConfirmStop] = useState(false);
  const latest = service.latestDeployment;

  const run: Run = (mutate, done, failed, after) =>
    mutate({
      onSuccess: () => {
        after?.();
        toast.add({ title: done, description: service.name, type: "success" });
      },
      onError: (err) => toast.add({ title: failed, description: errorMessage(err), type: "error" }),
    });

  if (service.restoreFence) return null;

  if (latest && isPending(latest.status)) {
    return (
      <Button
        loading={cancel.isPending}
        onClick={() =>
          run((o) => cancel.mutate(latest.id, o), "Deployment canceled", "Couldn't cancel")
        }
      >
        Cancel
      </Button>
    );
  }

  if (service.status === "stopped") {
    // Redeploy the deployment that was running; a service stopped before
    // its first deploy gets a fresh one.
    const onRedeploy = () =>
      latest?.image
        ? run(
            (o) => redeploy.mutate(latest.id, o),
            "Redeploy queued",
            "Couldn't redeploy",
            hint.clear,
          )
        : run(
            (o) => deploy.mutate(undefined, o),
            "Deployment queued",
            "Couldn't deploy",
            hint.clear,
          );
    return (
      <>
        <Button loading={redeploy.isPending || deploy.isPending} onClick={onRedeploy}>
          Redeploy
        </Button>
        <Button
          variant="primary"
          loading={start.isPending}
          onClick={() =>
            run((o) => start.mutate(undefined, o), "Service started", "Couldn't start")
          }
        >
          Start
        </Button>
      </>
    );
  }

  if (service.status === "active" || service.status === "crashed") {
    return (
      <>
        <Button
          loading={restart.isPending}
          onClick={() =>
            run((o) => restart.mutate(undefined, o), "Service restarted", "Couldn't restart")
          }
        >
          Restart
        </Button>
        <Button variant="destructive" onClick={() => setConfirmStop(true)}>
          Stop
        </Button>
        <ConfirmDialog
          open={confirmStop}
          onOpenChange={setConfirmStop}
          title={`Stop ${service.name}?`}
          confirmLabel="Stop service"
          pending={stop.isPending}
          error={stop.error ? errorMessage(stop.error) : undefined}
          onConfirm={() =>
            run(
              (o) => stop.mutate(undefined, o),
              "Service stopped",
              "Couldn't stop",
              () => setConfirmStop(false),
            )
          }
        >
          Its container stops and its domains stop serving traffic until you start it again. Data in
          volumes is kept, and the next deploy starts it automatically.
        </ConfirmDialog>
      </>
    );
  }

  return (
    <Button
      variant="primary"
      loading={deploy.isPending}
      onClick={() =>
        run((o) => deploy.mutate(undefined, o), "Deployment queued", "Couldn't deploy", hint.clear)
      }
    >
      Deploy
    </Button>
  );
}
