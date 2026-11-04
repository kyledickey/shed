import { Button, Input, Switch } from "@cloudflare/kumo";
import { CheckCircleIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useSaveBackupSettings, useTestBackupSettings } from "../../api/backups";
import { errorMessage } from "../../api/client";
import type { BackupSettings, BackupSettingsInput, S3Settings } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { ConfirmDialog } from "../settings/ConfirmDialog";

import styles from "../settings/Form.module.css";
import { SettingsForm } from "../settings/SectionForm";

type Draft = Omit<S3Settings, "hasSecret"> & { secretAccessKey: string };
type S3Input = NonNullable<BackupSettingsInput["s3"]>;

const emptyDraft: Draft = {
  endpoint: "",
  region: "",
  bucket: "",
  prefix: "",
  accessKeyId: "",
  secretAccessKey: "",
  pathStyle: false,
};

const toDraft = (s3: S3Settings | null): Draft =>
  s3 ? { ...s3, secretAccessKey: "" } : emptyDraft;

const toInput = (d: Draft): S3Input => ({
  endpoint: d.endpoint.trim(),
  region: d.region.trim(),
  bucket: d.bucket.trim(),
  prefix: d.prefix.trim(),
  accessKeyId: d.accessKeyId.trim(),
  pathStyle: d.pathStyle,
  secretAccessKey: d.secretAccessKey || undefined,
});

function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value.trim());
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

/** S3Card edits the global S3 destination and tests it before saving: rows for a server section. */
export function S3Card({ settings }: { settings: BackupSettings }) {
  const saved = toDraft(settings.s3);
  const hasSecret = settings.s3?.hasSecret ?? false;
  const [draft, setDraft] = useState(saved);
  const [removing, setRemoving] = useState(false);
  const save = useSaveBackupSettings();
  const remove = useSaveBackupSettings();
  const test = useTestBackupSettings();
  const toast = useToast();

  const encryption = { enabled: settings.encryption.enabled };
  const dirty = (Object.keys(saved) as (keyof Draft)[]).some((k) => saved[k] !== draft[k]);
  const complete =
    isHttpUrl(draft.endpoint) &&
    draft.bucket.trim() !== "" &&
    draft.accessKeyId.trim() !== "" &&
    (hasSecret || draft.secretAccessKey !== "");

  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    test.reset();
    setDraft((d) => ({ ...d, [key]: value }));
  };

  const form = {
    dirty,
    pending: save.isPending,
    error: save.error,
    invalid: !complete,
    message: complete
      ? undefined
      : "An http(s) endpoint, a bucket, an access key ID, and a secret are required.",
    reset: () => {
      setDraft(saved);
      save.reset();
      test.reset();
    },
    save: () =>
      save.mutate(
        { s3: toInput(draft), encryption },
        {
          onSuccess: (next) => {
            setDraft(toDraft(next.s3));
            test.reset();
            toast.add({ type: "success", title: "S3 destination saved" });
          },
        },
      ),
  };

  const text = (key: Exclude<keyof Draft, "pathStyle">, label: string, placeholder?: string) => (
    <Input
      aria-label={label}
      className={cx(styles.wide, styles.mono)}
      value={draft[key]}
      onChange={(e) => set(key, e.target.value)}
      placeholder={placeholder}
      autoComplete="off"
      spellCheck={false}
    />
  );

  return (
    <>
      <SettingsForm form={form}>
        <SettingRow
          label="Endpoint URL"
          description="For example https://s3.us-east-1.amazonaws.com, or your MinIO or R2 URL."
          control={text("endpoint", "Endpoint URL", "https://s3.us-east-1.amazonaws.com")}
        />
        <SettingRow label="Region" control={text("region", "Region", "us-east-1")} />
        <SettingRow label="Bucket" control={text("bucket", "Bucket")} />
        <SettingRow
          label="Prefix"
          description="Optional folder inside the bucket."
          control={text("prefix", "Prefix", "shed")}
        />
        <SettingRow label="Access key ID" control={text("accessKeyId", "Access key ID")} />
        <SettingRow
          label="Secret access key"
          description={hasSecret ? "Leave blank to keep the saved secret." : undefined}
          control={
            <Input
              aria-label="Secret access key"
              className={cx(styles.wide, styles.mono)}
              type="password"
              value={draft.secretAccessKey}
              onChange={(e) => set("secretAccessKey", e.target.value)}
              placeholder={hasSecret ? "••••••• (saved)" : ""}
              autoComplete="new-password"
            />
          }
        />
        <SettingRow
          label="Path-style addressing"
          description="Use endpoint/bucket URLs instead of bucket.endpoint. MinIO and many self-hosted stores need this."
          control={
            <Switch
              aria-label="Path-style addressing"
              checked={draft.pathStyle}
              onCheckedChange={(v) => set("pathStyle", v)}
            />
          }
        />
      </SettingsForm>
      <SettingRow
        label="Connection"
        description={
          test.isSuccess ? (
            <span className={cx(styles.status, styles.statusOk)}>
              <CheckCircleIcon weight="fill" /> Connection works. shed could reach the bucket.
            </span>
          ) : test.error ? (
            <span className={cx(styles.status, styles.statusFailed)}>
              Connection failed: {errorMessage(test.error)}
            </span>
          ) : (
            "Check that shed can reach the bucket with these settings, before or after saving."
          )
        }
        control={
          <Button
            loading={test.isPending}
            disabled={!complete}
            onClick={() => test.mutate({ s3: toInput(draft), encryption })}
          >
            Test connection
          </Button>
        }
      />
      {settings.s3 && (
        <SettingRow
          label="Remove destination"
          description="New backups stay on this server only."
          control={
            <Button variant="secondary-destructive" onClick={() => setRemoving(true)}>
              Remove
            </Button>
          }
        />
      )}
      <ConfirmDialog
        open={removing}
        onOpenChange={(open) => {
          setRemoving(open);
          remove.reset();
        }}
        title="Remove S3 destination"
        confirmLabel="Remove destination"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          remove.mutate(
            { s3: null, encryption },
            {
              onSuccess: (next) => {
                setDraft(toDraft(next.s3));
                test.reset();
                setRemoving(false);
                toast.add({ type: "success", title: "S3 destination removed" });
              },
            },
          )
        }
      >
        New backups stay on this server only. Backups already in the bucket stay there and can still
        be downloaded, restored, and deleted. Policies that keep no local backups must be changed
        first.
      </ConfirmDialog>
    </>
  );
}
