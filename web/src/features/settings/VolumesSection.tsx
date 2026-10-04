import { Plus, Trash } from "lucide-react";
import { useState } from "react";
import { useAddVolume, useDeleteVolume } from "../../api/services";
import type { Service, Volume } from "../../api/types";
import { ErrorText } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Input } from "../../components/Input";
import { RelativeTime } from "../../components/RelativeTime";
import { useRedeployHint } from "../services/redeploy";
import styles from "./Settings.module.css";

export function VolumesSection({ service }: { service: Service }) {
  const add = useAddVolume(service.id);
  const remove = useDeleteVolume(service.id);
  const hint = useRedeployHint();
  const [path, setPath] = useState("");
  const [removing, setRemoving] = useState<Volume | null>(null);

  return (
    <Card
      title="Volumes"
      description="Persistent storage that survives deploys. Services with volumes restart without overlap."
    >
      {service.volumes.length > 0 && (
        <ul className={styles.list}>
          {service.volumes.map((v) => (
            <li key={v.id} className={styles.item}>
              <span className={styles.mono}>{v.mountPath}</span>
              <span className={styles.hint}>
                Created <RelativeTime iso={v.createdAt} />
              </span>
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={`Delete volume at ${v.mountPath}`}
                onClick={() => setRemoving(v)}
              >
                <Trash size={14} />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form
        className={styles.inline}
        onSubmit={(e) => {
          e.preventDefault();
          const mountPath = path.trim();
          if (!mountPath) return;
          add.mutate(mountPath, {
            onSuccess: () => {
              setPath("");
              hint.markPending();
            },
          });
        }}
      >
        <Input
          mono
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/data"
          aria-label="Mount path"
        />
        <Button type="submit" disabled={!path.trim().startsWith("/")} loading={add.isPending}>
          <Plus size={14} />
          Add volume
        </Button>
      </form>
      <ErrorText error={add.error} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
          remove.reset();
        }}
        title="Delete volume"
        confirmLabel="Delete volume"
        typeToConfirm={removing?.mountPath}
        pending={remove.isPending}
        error={remove.error?.message}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.id, {
            onSuccess: () => {
              setRemoving(null);
              hint.markPending();
            },
          })
        }
      >
        <p>
          All data stored at <code>{removing?.mountPath}</code> will be permanently deleted. This
          can't be undone.
        </p>
      </ConfirmDialog>
    </Card>
  );
}
