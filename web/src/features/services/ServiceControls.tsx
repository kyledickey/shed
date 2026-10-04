import { Play, Rocket, RotateCw, Square } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useDeploy } from "../../api/deployments";
import { useRestartService, useStartService, useStopService } from "../../api/services";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { useToast } from "../../components/Overlay";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { useRedeployHint } from "./redeploy";

type Mutation = {
  mutate: (
    variables: undefined,
    options: { onSuccess: () => void; onError: (err: Error) => void },
  ) => void;
};

/** ServiceControls are the run actions of a service: restart, stop or start, and deploy. */
export function ServiceControls({ service }: { service: Service }) {
  const toast = useToast();
  const hint = useRedeployHint();
  const deploy = useDeploy(service.id);
  const stop = useStopService(service.id);
  const start = useStartService(service.id);
  const restart = useRestartService(service.id);
  const [confirmStop, setConfirmStop] = useState(false);
  const running = service.status === "active" || service.status === "crashed";

  const run = (m: Mutation, done: string, failed: string, after?: () => void) =>
    m.mutate(undefined, {
      onSuccess: () => {
        after?.();
        toast.add({ title: done, description: service.name, type: "success" });
      },
      onError: (err) => toast.add({ title: failed, description: errorMessage(err), type: "error" }),
    });

  return (
    <>
      {service.status === "stopped" ? (
        <Button
          size="sm"
          loading={start.isPending}
          onClick={() => run(start, "Service started", "Couldn't start")}
        >
          {!start.isPending && <Play size={13} />}
          Start
        </Button>
      ) : (
        running && (
          <>
            <Button
              size="sm"
              loading={restart.isPending}
              onClick={() => run(restart, "Service restarted", "Couldn't restart")}
            >
              {!restart.isPending && <RotateCw size={13} />}
              Restart
            </Button>
            <Button size="sm" onClick={() => setConfirmStop(true)}>
              <Square size={12} />
              Stop
            </Button>
          </>
        )
      )}
      <Button
        size="sm"
        variant="primary"
        loading={deploy.isPending}
        onClick={() => run(deploy, "Deployment queued", "Couldn't deploy", () => hint.clear())}
      >
        {!deploy.isPending && <Rocket size={13} />}
        Deploy
      </Button>
      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={`Stop ${service.name}?`}
        confirmLabel="Stop service"
        pending={stop.isPending}
        error={stop.error ? errorMessage(stop.error) : undefined}
        onConfirm={() => run(stop, "Service stopped", "Couldn't stop", () => setConfirmStop(false))}
      >
        Its container stops and its domains stop serving traffic until you start it again. Data in
        volumes is kept, and the next deploy starts it automatically.
      </ConfirmDialog>
    </>
  );
}
