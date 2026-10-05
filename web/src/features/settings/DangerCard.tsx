import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useDeleteService } from "../../api/services";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { useToast } from "../../components/Overlay";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./Settings.module.css";

/** DangerCard deletes the service after the user types its name. */
export function DangerCard({ service }: { service: Service }) {
  const [confirming, setConfirming] = useState(false);
  const remove = useDeleteService(service);
  const navigate = useNavigate();
  const toast = useToast();

  return (
    <LayerCard title="Danger zone">
      <div className={styles.section}>
        <div className={styles.danger}>
          <p className={styles.note}>
            Delete this service with its containers, images, and volumes.
          </p>
          <Button variant="danger" onClick={() => setConfirming(true)}>
            Delete service
          </Button>
        </div>
      </div>
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
    </LayerCard>
  );
}
