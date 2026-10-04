import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useSaveBackupPolicy } from "../../api/backups";
import type { BackupCompression, BackupPolicy, BackupPolicyInput } from "../../api/types";
import { Field, Input, Segmented, Select, Switch } from "../../components/Form";
import { useToast } from "../../components/Overlay";
import { presetFor, schedulePresets } from "../../lib/cron";
import { formatDateUTC, relativeTime } from "../../lib/time";
import { SettingsCard } from "../settings/SectionForm";
import styles from "../settings/Settings.module.css";

const compressionOptions: { value: BackupCompression; label: string; hint: string }[] = [
  { value: "fastest", label: "Fastest", hint: "Least CPU, largest archives." },
  { value: "default", label: "Default", hint: "A balance of speed and size." },
  { value: "better", label: "Better", hint: "Smaller archives, more CPU." },
  { value: "best", label: "Best", hint: "Smallest archives, slowest. Suits backups kept long." },
];

type ScheduleMode = "custom" | (typeof schedulePresets)[number]["id"];

const pick = (p: BackupPolicy): BackupPolicyInput => ({
  enabled: p.enabled,
  schedule: p.schedule,
  compression: p.compression,
  keepLocal: p.keepLocal,
  upload: p.upload,
  keepRemote: p.keepRemote,
});

/** validatePolicy returns the first problem with a draft policy, or null when it can be saved. */
export function validatePolicy(draft: BackupPolicyInput, s3Configured: boolean): string | null {
  const uploads = draft.upload && s3Configured;
  if (draft.enabled && !draft.schedule.trim()) return "Enter a cron schedule.";
  if (!Number.isInteger(draft.keepLocal) || draft.keepLocal < 0) {
    return "Local backups to keep must be a whole number.";
  }
  if (draft.keepLocal === 0 && !uploads) {
    return "Keep at least one local backup, or upload to S3 to keep none.";
  }
  if (uploads && (!Number.isInteger(draft.keepRemote) || draft.keepRemote < 1)) {
    return "Keep at least one backup in S3.";
  }
  return null;
}

type PolicyCardProps = {
  /** A service id, or null for shed's own database. */
  scope: string | null;
  policy: BackupPolicy;
  /** Whether an S3 destination is configured. */
  s3Configured: boolean;
};

/** PolicyCard edits a backup schedule and retention policy and saves it as a whole. */
export function PolicyCard({ scope, policy, s3Configured }: PolicyCardProps) {
  const saved = pick(policy);
  const [draft, setDraft] = useState(saved);
  const [custom, setCustom] = useState(presetFor(saved.schedule) === "custom");
  const save = useSaveBackupPolicy(scope);
  const toast = useToast();

  const set = <K extends keyof BackupPolicyInput>(key: K, value: BackupPolicyInput[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));
  const dirty = (Object.keys(saved) as (keyof BackupPolicyInput)[]).some(
    (k) => !Object.is(saved[k], draft[k]),
  );
  const problem = validatePolicy(draft, s3Configured);
  const uploads = draft.upload && s3Configured;
  const mode: ScheduleMode = custom ? "custom" : presetFor(draft.schedule);

  const pickMode = (next: ScheduleMode) => {
    if (next === "custom") return setCustom(true);
    setCustom(false);
    set("schedule", schedulePresets.find((p) => p.id === next)!.cron);
  };

  const form = {
    dirty,
    pending: save.isPending,
    error: save.error,
    invalid: problem !== null,
    reset: () => {
      setDraft(saved);
      setCustom(presetFor(saved.schedule) === "custom");
      save.reset();
    },
    save: () => {
      if (problem) return;
      save.mutate(
        { ...draft, schedule: draft.schedule.trim() },
        { onSuccess: () => toast.add({ type: "success", title: "Backup policy saved" }) },
      );
    },
  };

  return (
    <SettingsCard title="Policy" form={form}>
      <Switch
        checked={draft.enabled}
        onChange={(v) => set("enabled", v)}
        label="Scheduled backups"
        description="Back up automatically on a schedule."
      />
      {draft.enabled && (
        <Field
          label="Schedule"
          hint={<ScheduleHint policy={policy} draft={draft} custom={mode === "custom"} />}
        >
          {(id) => (
            <div className={styles.fields}>
              <div>
                <Segmented
                  label="Schedule"
                  value={mode}
                  onChange={pickMode}
                  options={[
                    ...schedulePresets.map((p) => ({
                      value: p.id,
                      label: p.label,
                      title: p.title,
                    })),
                    { value: "custom" as const, label: "Custom" },
                  ]}
                />
              </div>
              {mode === "custom" && (
                <Input
                  id={id}
                  mono
                  value={draft.schedule}
                  onChange={(e) => set("schedule", e.target.value)}
                  placeholder="0 3 * * *"
                  autoComplete="off"
                  spellCheck={false}
                />
              )}
            </div>
          )}
        </Field>
      )}
      <Field
        label="Compression"
        hint={compressionOptions.find((o) => o.value === draft.compression)?.hint}
      >
        {(id) => (
          <Select
            id={id}
            value={draft.compression}
            onChange={(v) => set("compression", v as BackupCompression)}
            options={compressionOptions}
          />
        )}
      </Field>
      <Field
        label="Keep locally"
        hint="Scheduled backups only. Manual backups are kept until deleted."
      >
        {(id) => (
          <Input
            id={id}
            type="number"
            min={0}
            step={1}
            suffix="backups"
            value={Number.isNaN(draft.keepLocal) ? "" : draft.keepLocal}
            onChange={(e) => set("keepLocal", e.target.valueAsNumber)}
          />
        )}
      </Field>
      <Switch
        checked={uploads}
        disabled={!s3Configured}
        onChange={(v) => set("upload", v)}
        label="Upload to S3"
        description={
          s3Configured ? (
            "Copy each backup to the S3 destination."
          ) : (
            <>
              No S3 destination is configured. Add one in{" "}
              <Link to="/settings" className={styles.host}>
                settings
              </Link>
              .
            </>
          )
        }
      />
      {uploads && (
        <Field label="Keep in S3">
          {(id) => (
            <Input
              id={id}
              type="number"
              min={1}
              step={1}
              suffix="backups"
              value={Number.isNaN(draft.keepRemote) ? "" : draft.keepRemote}
              onChange={(e) => set("keepRemote", e.target.valueAsNumber)}
            />
          )}
        </Field>
      )}
      {dirty && problem && <p className={styles.error}>{problem}</p>}
    </SettingsCard>
  );
}

function ScheduleHint({
  policy,
  draft,
  custom,
}: {
  policy: BackupPolicy;
  draft: BackupPolicyInput;
  custom: boolean;
}) {
  const unchanged = policy.enabled && policy.schedule === draft.schedule;
  return (
    <>
      {custom
        ? "Standard 5-field cron, in UTC unless it starts with CRON_TZ=<zone>."
        : "Times are UTC."}{" "}
      {unchanged && policy.nextRunAt ? (
        <>
          Next run {relativeTime(policy.nextRunAt)} ({formatDateUTC(policy.nextRunAt)}).
        </>
      ) : (
        "Save to see the next run."
      )}
    </>
  );
}
