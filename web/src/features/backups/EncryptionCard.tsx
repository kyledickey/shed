import { KeyRound, TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { useRevealBackupKey, useSaveBackupSettings } from "../../api/backups";
import { errorMessage } from "../../api/client";
import type { BackupSettings } from "../../api/types";
import { Button } from "../../components/Button";
import { Switch } from "../../components/Form";
import { Callout, CopyButton } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import { SettingsCard } from "../settings/SectionForm";
import styles from "./Backups.module.css";
import settingsStyles from "../settings/Settings.module.css";

/** EncryptionCard turns age encryption of new backups on or off and reveals the secret key. */
export function EncryptionCard({ settings }: { settings: BackupSettings }) {
  const saved = settings.encryption.enabled;
  const [enabled, setEnabled] = useState(saved);
  const [revealing, setRevealing] = useState(false);
  const save = useSaveBackupSettings();
  const toast = useToast();
  const { recipient } = settings.encryption;

  const form = {
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
    <SettingsCard title="Encryption" meta="Protect archives with age." form={form}>
      <Switch
        checked={enabled}
        onChange={setEnabled}
        label="Encrypt new backups"
        description="Every new archive, local and in S3, is encrypted to shed's key. Older archives keep the encryption they were created with."
      />
      {recipient && (
        <div className={settingsStyles.fields}>
          <div>
            <p className={settingsStyles.subhead}>Public key</p>
            <div className={styles.recipient}>
              <span className={styles.recipientValue} title={recipient}>
                {recipient}
              </span>
              <CopyButton value={recipient} label="Copy public key" />
            </div>
          </div>
          <div>
            <Button size="sm" onClick={() => setRevealing(true)}>
              <KeyRound size={13} /> Reveal secret key
            </Button>
          </div>
        </div>
      )}
      {saved && (
        <Callout
          tone="sunflower"
          icon={<TriangleAlert size={16} />}
          title="Keep the secret key safe"
        >
          Without the secret key, encrypted backups can't be recovered, including those of shed's
          own database. Store a copy off this server.
        </Callout>
      )}
      <KeyDialog open={revealing} onOpenChange={setRevealing} />
    </SettingsCard>
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
  // Fetch when the dialog opens and drop the key when it closes.
  useEffect(() => {
    if (open) mutate();
    else reset();
  }, [open, mutate, reset]);

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Secret key"
      icon={<KeyRound size={18} />}
      iconTone="sunflower"
      size="md"
      footer={<Button onClick={() => onOpenChange(false)}>Done</Button>}
    >
      <div className={settingsStyles.fields}>
        <Callout tone="tomato" icon={<TriangleAlert size={16} />} title="Store this off the server">
          Anyone with this key can read your backups. If you lose it, and this server's disk is lost
          too, encrypted backups can't be recovered. Save it in a password manager now.
        </Callout>
        {reveal.error ? (
          <p className={settingsStyles.error}>{errorMessage(reveal.error)}</p>
        ) : (
          <div className={styles.keyBlock}>
            <pre className={styles.key}>{reveal.data?.identity ?? "Loading…"}</pre>
            {reveal.data && <CopyButton value={reveal.data.identity} label="Copy secret key" />}
          </div>
        )}
      </div>
    </Dialog>
  );
}
