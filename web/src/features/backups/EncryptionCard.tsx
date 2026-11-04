import { Banner, Button, ClipboardText, Switch } from "@cloudflare/kumo";
import { CopyIcon, KeyIcon, WarningIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useRevealBackupKey, useSaveBackupSettings } from "../../api/backups";
import { errorMessage } from "../../api/client";
import type { BackupSettings } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow } from "../../components/Settings";
import { HelpTip } from "../docs/HelpTip";

import form from "../settings/Form.module.css";
import { FormDialog } from "../settings/FormDialog";
import { SettingsForm } from "../settings/SectionForm";
import styles from "./Encryption.module.css";

/** EncryptionCard turns age encryption of new backups on or off and reveals the secret key. */
export function EncryptionCard({ settings }: { settings: BackupSettings }) {
  const saved = settings.encryption.enabled;
  const [enabled, setEnabled] = useState(saved);
  const [revealing, setRevealing] = useState(false);
  const save = useSaveBackupSettings();
  const toast = useToast();
  const { recipient } = settings.encryption;

  const state = {
    dirty: enabled !== saved,
    pending: save.isPending,
    error: save.error,
    reset: () => {
      setEnabled(saved);
      save.reset();
    },
    save: () =>
      save.mutate(
        {
          s3: settings.s3 && {
            endpoint: settings.s3.endpoint,
            region: settings.s3.region,
            bucket: settings.s3.bucket,
            prefix: settings.s3.prefix,
            accessKeyId: settings.s3.accessKeyId,
            pathStyle: settings.s3.pathStyle,
          },
          encryption: { enabled },
        },
        {
          onSuccess: (next) => {
            toast.add({
              type: "success",
              title: next.encryption.enabled ? "Encryption enabled" : "Encryption disabled",
            });
            if (next.encryption.enabled) setRevealing(true);
          },
        },
      ),
  };

  return (
    <>
      <SettingsForm form={state}>
        <SettingRow
          label={<HelpTip topic="backupEncryption">Encrypt new backups</HelpTip>}
          description="Every new archive, local and in S3, is encrypted with age to shed's key. Older archives keep the encryption they were created with."
          control={
            <Switch
              aria-label="Encrypt new backups"
              checked={enabled}
              onCheckedChange={setEnabled}
            />
          }
        />
      </SettingsForm>
      {recipient && (
        <SettingRow
          label="Public key"
          description="Archives are encrypted to this recipient."
          control={<ClipboardText text={recipient} size="base" className={styles.recipient} />}
        />
      )}
      {recipient && (
        <SettingRow
          label="Secret key"
          description={
            saved
              ? "Without the secret key, encrypted backups can't be recovered, including those of shed's own database. Store a copy off this server."
              : "Decrypts archives made while encryption was on."
          }
          control={
            <Button icon={<KeyIcon />} onClick={() => setRevealing(true)}>
              Reveal secret key
            </Button>
          }
        />
      )}
      <KeyDialog open={revealing} onOpenChange={setRevealing} />
    </>
  );
}

function KeyDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const reveal = useRevealBackupKey();
  const { mutate, reset } = reveal;
  const [copied, setCopied] = useState(false);
  // Fetch when the dialog opens and drop the key when it closes.
  useEffect(() => {
    if (open) mutate();
    else {
      reset();
      setCopied(false);
    }
  }, [open, mutate, reset]);

  const identity = reveal.data?.identity;
  return (
    <FormDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Secret key"
      size="xl"
      footer={
        <>
          <Button
            icon={<CopyIcon />}
            disabled={!identity}
            onClick={() =>
              identity && void navigator.clipboard.writeText(identity).then(() => setCopied(true))
            }
          >
            {copied ? "Copied" : "Copy secret key"}
          </Button>
          <Button variant="primary" onClick={() => onOpenChange(false)}>
            Done
          </Button>
        </>
      }
    >
      <Banner
        variant="alert"
        icon={<WarningIcon weight="fill" />}
        title="Store this off the server"
        description="Anyone with this key can read your backups. If you lose it, and this server's disk is lost too, encrypted backups can't be recovered. Save it in a password manager now."
      />
      {reveal.error ? (
        <p className={form.error}>{errorMessage(reveal.error)}</p>
      ) : (
        <pre className={styles.key}>{identity ?? "Loading…"}</pre>
      )}
    </FormDialog>
  );
}
