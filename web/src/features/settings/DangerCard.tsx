import { Button } from "@cloudflare/kumo";
import { TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useDeleteService } from "../../api/services";
import type { Service } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow } from "../../components/Settings";
import { ConfirmDialog } from "./ConfirmDialog";

/** DangerSection deletes the service after the user types its name. */
export function DangerSection({ service }: { service: Service }) {
  const [confirming, setConfirming] = useState(false);
  const remove = useDeleteService(service);
  const navigate = useNavigate();
  const toast = useToast();

  return (
    <SettingRow
      label="Delete service"
      description="Stop and remove this service with its containers, images, and volumes."
      control={
        <Button variant="destructive" icon={<TrashIcon />} onClick={() => setConfirming(true)}>
          Delete
        </Button>
      }
    >
      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          setConfirming(open);
          remove.reset();
        }}
        title="Delete service"
        confirmLabel="Delete service"
        typeToConfirm={service.name}
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          remove.mutate(undefined, {
            onSuccess: () => {
              toast.add({ type: "success", title: "Service deleted", description: service.name });
              void navigate({
                to: "/projects/$projectId",
                params: { projectId: service.projectId },
              });
            },
          })
        }
      >
        <strong>{service.name}</strong> will be stopped and removed along with all of its volumes
        and data. This can't be undone.
      </ConfirmDialog>
    </SettingRow>
  );
}
