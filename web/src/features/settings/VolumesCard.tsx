import { HardDrive, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useAddVolume, useDeleteVolume } from "../../api/services";
import type { Service, Volume } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input } from "../../components/Form";
import { ServiceIcon } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import { useRedeployHint } from "../services/redeploy";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./Settings.module.css";

/** VolumesCard lists persistent volumes and adds or removes them. */
export function VolumesCard({ service }: { service: Service }) {
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Volume | null>(null);
  const remove = useDeleteVolume(service.id);
  const hint = useRedeployHint();
  const toast = useToast();

  return (
    <LayerCard
      title="Volumes"
      meta="Data that survives redeploys."
      actions={
        <Button size="sm" onClick={() => setAdding(true)}>
          <Plus size={13} /> Add volume
        </Button>
      }
    >
      {service.volumes.length === 0 ? (
        <div className={styles.section}>
          <p className={styles.note}>
            No volumes. Files written by the container are lost on redeploy.
          </p>
        </div>
      ) : (
        <div className={styles.list}>
          {service.volumes.map((v) => (
            <div key={v.id} className={styles.row}>
              <ServiceIcon icon={<HardDrive />} tone="neutral" size={28} />
              <div className={styles.rowMain}>
                <span className={styles.mono}>{v.mountPath}</span>
                <span className={styles.muted}>
                  Created {new Date(v.createdAt).toLocaleDateString()}
                </span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={`Remove ${v.mountPath}`}
                onClick={() => setRemoving(v)}
              >
                <Trash2 size={14} />
              </Button>
            </div>
          ))}
        </div>
      )}
      <AddVolumeDialog service={service} open={adding} onOpenChange={setAdding} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
          remove.reset();
        }}
        title="Remove volume"
        confirmLabel="Remove volume"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.id, {
            onSuccess: () => {
              hint.markPending();
              toast.add({
                type: "success",
                title: "Volume removed",
                description: "Redeploy to apply.",
              });
              setRemoving(null);
            },
          })
        }
      >
        The volume mounted at <strong>{removing?.mountPath}</strong> and all data on it will be
        permanently deleted.
      </ConfirmDialog>
    </LayerCard>
  );
}

function AddVolumeDialog({
  service,
  open,
  onOpenChange,
}: {
  service: Service;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const add = useAddVolume(service.id);
  const hint = useRedeployHint();
  const toast = useToast();
  const [path, setPath] = useState("");
  const mountPath = path.trim();
  const valid = mountPath.startsWith("/") && mountPath.length > 1;

  const submit = () =>
    add.mutate(mountPath, {
      onSuccess: () => {
        hint.markPending();
        toast.add({ type: "success", title: "Volume added", description: "Redeploy to apply." });
        setPath("");
        onOpenChange(false);
      },
    });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) add.reset();
        onOpenChange(next);
      }}
      title="Add volume"
      footer={
        <>
          <Button onClick={() => onOpenChange(false)} disabled={add.isPending}>
            Cancel
          </Button>
          <Button variant="primary" loading={add.isPending} disabled={!valid} onClick={submit}>
            Add volume
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) submit();
        }}
      >
        <Field
          label="Mount path"
          hint="Absolute path inside the container."
          error={add.error ? errorMessage(add.error) : undefined}
        >
          {(id) => (
            <Input
              id={id}
              mono
              autoFocus
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/data"
            />
          )}
        </Field>
      </form>
    </Dialog>
  );
}
