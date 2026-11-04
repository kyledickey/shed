import { Button } from "@cloudflare/kumo";
import { ShieldWarningIcon } from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { serviceBackupsQuery } from "../../api/backups";
import { errorMessage } from "../../api/client";
import { useClearRestoreFence } from "../../api/services";
import type { RestoreFence, Service } from "../../api/types";
import { Notice } from "../../components/Notice";
import { useToast } from "../../components/Overlay";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { HelpTip } from "../docs/HelpTip";

/**
 * RestoreFenceCallout explains why a service that a failed restore fenced
 * cannot run, and lets the user keep its current data by clearing the fence.
 * It shows nothing while the fence's restore is still running.
 */
export function RestoreFenceCallout({ service }: { service: Service }) {
  const fence = service.restoreFence;
  if (!fence) return null;
  return <FencedCallout service={service} fence={fence} />;
}

function FencedCallout({ service, fence }: { service: Service; fence: RestoreFence }) {
  const toast = useToast();
  const clear = useClearRestoreFence(service.id);
  const [confirm, setConfirm] = useState(false);
  const { data } = useQuery(serviceBackupsQuery(service.id));
  const restore = data?.restore;
  if (!data || (restore?.id === fence.restoreId && restore.status === "running")) return null;

  const onConfirm = () =>
    clear.mutate(undefined, {
      onSuccess: () => {
        setConfirm(false);
        toast.add({ title: "Restore fence cleared", description: service.name, type: "success" });
      },
    });

  return (
    <>
      <Notice
        danger
        icon={<ShieldWarningIcon size={16} />}
        title={
          <>
            <HelpTip topic="restoreFences">Restore failed</HelpTip>
          </>
        }
        actions={
          <Button size="sm" onClick={() => setConfirm(true)}>
            Keep current data
          </Button>
        }
      >
        Its data may be incomplete, so it can't be started or deployed. Restarting shed retries
        recovering it.
      </Notice>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title={`Keep the current data of ${service.name}?`}
        confirmLabel="Keep current data"
        pending={clear.isPending}
        error={clear.error ? errorMessage(clear.error) : undefined}
        onConfirm={onConfirm}
      >
        The data may be only partly restored. Clearing the fence keeps it as it is: the service
        stays stopped until you start or deploy it, and shed no longer tries to put the previous
        data back. Copies of the previous data in pre-restore volumes are left for you to inspect.
      </ConfirmDialog>
    </>
  );
}
