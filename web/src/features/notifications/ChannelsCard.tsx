import { Button, DropdownMenu, Input, Select, Switch } from "@cloudflare/kumo";
import { DotsThreeIcon, PencilSimpleIcon, PlusIcon, TrashIcon } from "@phosphor-icons/react";
import { useId, useState } from "react";
import { errorMessage } from "../../api/client";
import {
  useCreateNotificationChannel,
  useDeleteNotificationChannel,
  useTestNotificationChannel,
  useUpdateNotificationChannel,
} from "../../api/notifications";
import type { NotificationChannel } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { Pill } from "../../components/Pill";
import { Rows, SettingRow, Well } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { ConfirmDialog } from "../settings/ConfirmDialog";

import form from "../settings/Form.module.css";
import { FormDialog } from "../settings/FormDialog";
import { Group } from "../settings/Group";
import styles from "./Notifications.module.css";
import { type Field, targetForm } from "./targets";

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
    <Group
      title="Channels"
      description="Where notifications are sent, such as your phone through Hark or ntfy, or email."
      actions={
        channels.length > 0 && (
          <Button size="sm" icon={<PlusIcon />} onClick={() => setEditing("new")}>
            Add channel
          </Button>
        )
      }
    >
      {channels.length === 0 ? (
        <Well
          empty
          action={
            <Button size="sm" icon={<PlusIcon />} onClick={() => setEditing("new")}>
              Add channel
            </Button>
          }
        >
          No channels yet.
        </Well>
      ) : (
        <Rows>
          {channels.map((c) => (
            <ChannelRow
              key={c.id}
              channel={c}
              onEdit={() => setEditing(c)}
              onDelete={() => setDeleting(c)}
            />
          ))}
        </Rows>
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
        <strong>{deleting?.name}</strong> stops receiving notifications, and its delivery history is
        deleted.
      </ConfirmDialog>
    </Group>
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
    <SettingRow
      label={
        <>
          {c.name} <Pill>{c.provider}</Pill>
        </>
      }
      description={
        (test.isSuccess || test.isError || update.isError) && (
          <span aria-live="polite">
            {test.isSuccess && "Test notification sent."}
            {test.isError && <span className={styles.problem}>{errorMessage(test.error)}</span>}
            {update.isError && <span className={styles.problem}>{errorMessage(update.error)}</span>}
          </span>
        )
      }
      control={
        <>
          <Button size="sm" loading={test.isPending} onClick={() => test.mutate(c.id)}>
            Send test
          </Button>
          <Switch
            aria-label={`${c.name} enabled`}
            checked={c.enabled}
            disabled={update.isPending}
            onCheckedChange={(enabled) => update.mutate({ id: c.id, patch: { enabled } })}
          />
          <DropdownMenu>
            <DropdownMenu.Trigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  shape="square"
                  icon={<DotsThreeIcon weight="bold" />}
                  aria-label={`Actions for ${c.name}`}
                />
              }
            />
            <DropdownMenu.Content align="end">
              <DropdownMenu.Item icon={<PencilSimpleIcon />} onClick={onEdit}>
                Edit…
              </DropdownMenu.Item>
              <DropdownMenu.Item variant="danger" icon={<TrashIcon />} onClick={onDelete}>
                Delete…
              </DropdownMenu.Item>
            </DropdownMenu.Content>
          </DropdownMenu>
        </>
      }
    />
  );
}

/** ChannelDialog adds a channel, or edits one: the provider is fixed and blank target fields keep the saved target. */
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
  const [values, setValues] = useState<Record<string, string>>({});
  const create = useCreateNotificationChannel();
  const update = useUpdateNotificationChannel();
  const toast = useToast();
  const op = channel ? update : create;
  const targetFields = targetForm(provider).fields;
  const value = (f: Field) => values[f.key] ?? f.options?.[0]?.value ?? "";
  // The saved target is never returned, so an edit with every field blank keeps it.
  const keepTarget =
    channel !== null && targetFields.every((f) => f.options || value(f).trim() === "");
  let target: string | undefined;
  let targetError: string | undefined;
  if (!keepTarget && targetFields.every((f) => f.optional || value(f).trim() !== "")) {
    try {
      target = targetForm(provider).compose(
        Object.fromEntries(targetFields.map((f) => [f.key, value(f)])),
      );
    } catch (e) {
      targetError = e instanceof Error ? e.message : String(e);
    }
  }
  const valid = name.trim() !== "" && provider !== "" && (keepTarget || target !== undefined);

  const submit = () => {
    const done = () => {
      onClose();
      toast.add({ type: "success", title: channel ? "Channel saved" : "Channel added" });
    };
    if (channel) {
      update.mutate({ id: channel.id, patch: { name: name.trim(), target } }, { onSuccess: done });
    } else {
      create.mutate(
        { name: name.trim(), provider, target: target ?? "", enabled: true },
        { onSuccess: done },
      );
    }
  };

  return (
    <FormDialog
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
        className={styles.dialogForm}
        onSubmit={(e) => {
          e.preventDefault();
          if (valid) submit();
        }}
      >
        <Input
          label="Name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="My phone"
          autoComplete="off"
          autoFocus
        />
        <Select
          label="Provider"
          value={provider}
          onValueChange={(v) => {
            if (v) {
              setProvider(v);
              setValues({});
            }
          }}
          disabled={channel !== null}
          items={providers.map((p) => ({ value: p, label: p }))}
        />
        {channel && (
          <p className={form.note}>Leave the fields below blank to keep the saved target.</p>
        )}
        {targetFields.map((f) =>
          f.options ? (
            <Select
              key={`${provider}.${f.key}`}
              label={f.label}
              description={f.description}
              value={value(f)}
              onValueChange={(v) => v && setValues({ ...values, [f.key]: v })}
              items={f.options}
            />
          ) : (
            <Input
              key={`${provider}.${f.key}`}
              label={f.optional ? `${f.label} (optional)` : f.label}
              description={f.description}
              placeholder={f.placeholder}
              className={cx(form.mono)}
              type={f.secret ? "password" : "text"}
              value={value(f)}
              onChange={(e) => setValues({ ...values, [f.key]: e.target.value })}
              autoComplete={f.secret ? "new-password" : "off"}
              spellCheck={false}
            />
          ),
        )}
        {targetError && <p className={form.error}>{targetError}</p>}
        {op.error && <p className={form.error}>{errorMessage(op.error)}</p>}
      </form>
    </FormDialog>
  );
}
