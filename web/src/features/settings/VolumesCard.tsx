import { Button, Input, Table } from "@cloudflare/kumo";
import { PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useAddVolume, useDeleteVolume } from "../../api/services";
import type { Service, Volume } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow, Well } from "../../components/Settings";
import { HelpTip } from "../docs/HelpTip";
import { useRedeployHint } from "../services/redeploy";
import { ConfirmDialog } from "./ConfirmDialog";
import styles from "./Form.module.css";
import { FormDialog } from "./FormDialog";

/** VolumesSection lists persistent volumes and adds or removes them. */
export function VolumesSection({ service }: { service: Service }) {
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Volume | null>(null);
  const remove = useDeleteVolume(service.id);
  const hint = useRedeployHint();
  const toast = useToast();

  return (
    <SettingRow
      label={
        <>
          <HelpTip topic="volumes">Mounts</HelpTip>
        </>
      }
      description="Data under these paths survives redeploys."
      control={
        <Button icon={<PlusIcon />} onClick={() => setAdding(true)}>
          Add volume
        </Button>
      }
    >
      {service.volumes.length === 0 ? (
        <Well empty>No volumes. Files written by the container are lost on redeploy.</Well>
      ) : (
        <div className={styles.table}>
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Mount path</Table.Head>
                <Table.Head>Created</Table.Head>
                <Table.Head aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {service.volumes.map((v) => (
                <Table.Row key={v.id}>
                  <Table.Cell className={styles.cellMono}>{v.mountPath}</Table.Cell>
                  <Table.Cell className={styles.cellSubtle}>
                    {new Date(v.createdAt).toLocaleDateString()}
                  </Table.Cell>
                  <Table.Cell className={styles.cellActions}>
                    <Button
                      variant="ghost"
                      size="sm"
                      shape="square"
                      icon={<TrashIcon />}
                      aria-label={`Remove ${v.mountPath}`}
                      onClick={() => setRemoving(v)}
                    />
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
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
    </SettingRow>
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
    <FormDialog
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
        <Input
          label="Mount path"
          description="Absolute path inside the container."
          className={styles.mono}
          autoFocus
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="/data"
          spellCheck={false}
          error={add.error ? errorMessage(add.error) : undefined}
        />
      </form>
    </FormDialog>
  );
}
