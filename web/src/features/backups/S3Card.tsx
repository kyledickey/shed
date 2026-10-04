import { CircleCheck, CircleX } from "lucide-react";
import { useState } from "react";
import { useSaveBackupSettings, useTestBackupSettings } from "../../api/backups";
import { errorMessage } from "../../api/client";
import type { BackupSettings, BackupSettingsInput, S3Settings } from "../../api/types";
import { Button } from "../../components/Button";
import { Field, Input, Switch } from "../../components/Form";
import { Callout } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { SettingsCard } from "../settings/SectionForm";
import styles from "../settings/Settings.module.css";

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

/** S3Card edits the global S3 destination and tests it before saving. */
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

  return (
    <SettingsCard
      title="S3 destination"
      meta="Where backups are uploaded."
      form={form}
      footerStart={
        <>
          <Button
            size="sm"
            loading={test.isPending}
            disabled={!complete}
            onClick={() => test.mutate({ s3: toInput(draft), encryption })}
          >
            Test connection
          </Button>
          {settings.s3 && (
            <Button size="sm" variant="danger" onClick={() => setRemoving(true)}>
              Remove destination
            </Button>
          )}
        </>
      }
    >
      <Field
        label="Endpoint URL"
        hint="For example https://s3.us-east-1.amazonaws.com, or your MinIO or R2 URL."
      >
        {(id) => (
          <Input
            id={id}
            mono
            value={draft.endpoint}
            onChange={(e) => set("endpoint", e.target.value)}
            placeholder="https://s3.us-east-1.amazonaws.com"
            autoComplete="off"
            spellCheck={false}
          />
        )}
      </Field>
      <div className={styles.twoCol}>
        <Field label="Region">
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.region}
              onChange={(e) => set("region", e.target.value)}
              placeholder="us-east-1"
              autoComplete="off"
              spellCheck={false}
            />
          )}
        </Field>
        <Field label="Bucket">
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.bucket}
              onChange={(e) => set("bucket", e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          )}
        </Field>
      </div>
      <Field label="Prefix" hint="Optional folder inside the bucket.">
        {(id) => (
          <Input
            id={id}
            mono
            value={draft.prefix}
            onChange={(e) => set("prefix", e.target.value)}
            placeholder="shed"
            autoComplete="off"
            spellCheck={false}
          />
        )}
      </Field>
      <div className={styles.twoCol}>
        <Field label="Access key ID">
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.accessKeyId}
              onChange={(e) => set("accessKeyId", e.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          )}
        </Field>
        <Field
          label="Secret access key"
          hint={hasSecret ? "Leave blank to keep the saved secret." : undefined}
        >
          {(id) => (
            <Input
              id={id}
              mono
              type="password"
              value={draft.secretAccessKey}
              onChange={(e) => set("secretAccessKey", e.target.value)}
              placeholder={hasSecret ? "••••••• (saved)" : ""}
              autoComplete="new-password"
            />
          )}
        </Field>
      </div>
      <Switch
        checked={draft.pathStyle}
        onChange={(v) => set("pathStyle", v)}
        label="Path-style addressing"
        description="Use endpoint/bucket URLs instead of bucket.endpoint. MinIO and many self-hosted stores need this."
      />
      {test.isSuccess && (
        <Callout tone="grass" icon={<CircleCheck size={16} />} title="Connection works">
          shed could reach the bucket with these settings.
        </Callout>
      )}
      {test.error && (
        <Callout tone="tomato" icon={<CircleX size={16} />} title="Connection failed">
          {errorMessage(test.error)}
        </Callout>
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
        New backups stay on this server only. Objects already in the bucket are not deleted, and
        shed stops managing them. Policies that keep no local backups must be changed first.
      </ConfirmDialog>
    </SettingsCard>
  );
}
