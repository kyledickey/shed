import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useDeleteService } from "../../api/services";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import styles from "./Settings.module.css";

export function DangerZone({ service }: { service: Service }) {
  const [confirming, setConfirming] = useState(false);
  const remove = useDeleteService(service);
  const navigate = useNavigate();

  return (
    <Card title="Danger zone" tone="danger">
      <div className={styles.danger}>
        <p>Delete this service, its containers, images, and volumes.</p>
        <Button variant="danger" onClick={() => setConfirming(true)}>
          Delete service
        </Button>
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
        error={remove.error?.message}
        onConfirm={() =>
          remove.mutate(undefined, {
            onSuccess: () =>
              navigate({ to: "/projects/$projectId", params: { projectId: service.projectId } }),
          })
        }
      >
        <p>
          <strong>{service.name}</strong> will be stopped and removed along with all of its volumes
          and data. This can't be undone.
        </p>
      </ConfirmDialog>
    </Card>
  );
}
