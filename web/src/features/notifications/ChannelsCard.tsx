import { BellRing, Ellipsis, Pencil, Plus, Trash2 } from "lucide-react";
import { useId, useState } from "react";
import { errorMessage } from "../../api/client";
import {
  useCreateNotificationChannel,
  useDeleteNotificationChannel,
  useTestNotificationChannel,
  useUpdateNotificationChannel,
} from "../../api/notifications";
import type { NotificationChannel } from "../../api/types";
import { Badge, Count } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input, Select, Switch } from "../../components/Form";
import { EmptyState } from "../../components/Misc";
import { Dialog, Menu, MenuItem, useToast } from "../../components/Overlay";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import settings from "../settings/Settings.module.css";
import styles from "./Notifications.module.css";

/** ChannelsCard lists where notifications go, and adds, edits, tests, and deletes channels. */
export function ChannelsCard({
  channels,
  providers,
}: {
  channels: NotificationChannel[];
  providers: string[];
}) {
  // "new" opens the add dialog; a channel opens the edit dialog.
  const [editing, setEditing] = useState<NotificationChannel | "new" | null>(null);
  const [deleting, setDeleting] = useState<NotificationChannel | null>(null);
  const remove = useDeleteNotificationChannel();
  const toast = useToast();

  return (
    <LayerCard
      title="Channels"
      meta={<Count>{channels.length}</Count>}
      actions={
        <Button size="sm" onClick={() => setEditing("new")}>
          <Plus size={14} /> Add channel
        </Button>
      }
    >
      {channels.length === 0 ? (
        <EmptyState
          icon={<BellRing />}
          tone="neutral"
          title="No channels yet"
          description="A channel is where notifications are sent, such as your phone through Hark."
        />
      ) : (
        <div>
          {channels.map((c) => (
            <ChannelRow
              key={c.id}
              channel={c}
              onEdit={() => setEditing(c)}
              onDelete={() => setDeleting(c)}
            />
          ))}
        </div>
      )}
      {editing && (
        <ChannelDialog
          channel={editing === "new" ? null : editing}
          providers={providers}
          onClose={() => setEditing(null)}
        />
      )}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeleting(null);
            remove.reset();
          }
        }}
        title="Delete channel"
        confirmLabel="Delete"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          deleting &&
          remove.mutate(deleting.id, {
            onSuccess: () => {
              setDeleting(null);
              toast.add({ type: "success", title: "Channel deleted" });
            },
          })
        }
      >
        {deleting?.name} stops receiving notifications, and its delivery history is deleted.
      </ConfirmDialog>
    </LayerCard>
  );
}

function ChannelRow({
  channel: c,
  onEdit,
  onDelete,
}: {
  channel: NotificationChannel;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const update = useUpdateNotificationChannel();
  const test = useTestNotificationChannel();
  return (
    <div className={styles.row}>
      <div className={styles.main}>
        <span className={styles.name}>{c.name}</span>
        <span className={styles.meta} aria-live="polite">
          <Badge size="sm" variant="outline">
            {c.provider}
          </Badge>
          {test.isSuccess && <span className={styles.ok}>Test notification sent.</span>}
          {test.isError && <span className={styles.problem}>{errorMessage(test.error)}</span>}
          {update.isError && <span className={styles.problem}>{errorMessage(update.error)}</span>}
        </span>
      </div>
      <div className={styles.controls}>
        <Switch
          label="Enabled"
          checked={c.enabled}
          disabled={update.isPending}
          onChange={(enabled) => update.mutate({ id: c.id, patch: { enabled } })}
        />
        <Button size="sm" loading={test.isPending} onClick={() => test.mutate(c.id)}>
          Send test
        </Button>
        <Menu
          trigger={
            <Button variant="ghost" size="sm" icon aria-label={`Actions for ${c.name}`}>
              <Ellipsis size={14} />
            </Button>
          }
        >
          <MenuItem icon={<Pencil size={14} />} onClick={onEdit}>
            Edit…
          </MenuItem>
          <MenuItem icon={<Trash2 size={14} />} danger onClick={onDelete}>
            Delete…
          </MenuItem>
        </Menu>
      </div>
    </div>
  );
}

const targetHints: Record<string, string> = {
  hark: "The webhook URL from the Hark app, like https://hark.ryan.ceo/hooks/<token>. It works like a password.",
};

/** ChannelDialog adds a channel, or edits one: the provider is fixed and a blank target keeps the saved one. */
function ChannelDialog({
  channel,
  providers,
  onClose,
}: {
  channel: NotificationChannel | null;
  providers: string[];
  onClose: () => void;
}) {
  const formId = useId();
  const [name, setName] = useState(channel?.name ?? "");
  const [provider, setProvider] = useState(channel?.provider ?? providers[0] ?? "");
  const [target, setTarget] = useState("");
  const create = useCreateNotificationChannel();
  const update = useUpdateNotificationChannel();
  const toast = useToast();
  const op = channel ? update : create;
  const valid = name.trim() !== "" && provider !== "" && (channel !== null || target !== "");

  const submit = () => {
    const done = () => {
      onClose();
      toast.add({ type: "success", title: channel ? "Channel saved" : "Channel added" });
    };
    if (channel) {
      update.mutate(
        { id: channel.id, patch: { name: name.trim(), target: target || undefined } },
        { onSuccess: done },
      );
    } else {
      create.mutate({ name: name.trim(), provider, target, enabled: true }, { onSuccess: done });
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => !open && onClose()}
      title={channel ? "Edit channel" : "Add channel"}
      footer={
        <>
          <Button onClick={onClose} disabled={op.isPending}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            loading={op.isPending}
            disabled={!valid}
          >
            {channel ? "Save" : "Add"}
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className={settings.fields}
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) submit();
        }}
      >
        <Field label="Name">
          {(id) => (
            <Input
              id={id}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My phone"
              autoComplete="off"
              autoFocus
            />
          )}
        </Field>
        <Field label="Provider">
          {(id) => (
            <Select
              id={id}
              value={provider}
              onChange={setProvider}
              disabled={channel !== null}
              options={providers.map((p) => ({ value: p, label: p }))}
            />
          )}
        </Field>
        <Field
          label="Target"
          hint={
            channel
              ? "Leave blank to keep the saved target."
              : (targetHints[provider] ?? "Where to send notifications.")
          }
        >
          {(id) => (
            <Input
              id={id}
              mono
              type="password"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              autoComplete="new-password"
              spellCheck={false}
            />
          )}
        </Field>
        {op.error && <p className={settings.error}>{errorMessage(op.error)}</p>}
      </form>
    </Dialog>
  );
}
