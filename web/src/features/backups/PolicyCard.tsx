import { Button, InputGroup, Select, Switch, Tabs } from "@cloudflare/kumo";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useSaveBackupPolicy } from "../../api/backups";
import type { BackupCompression, BackupPolicy, BackupPolicyInput } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { Rows, SettingRow, Well } from "../../components/Settings";
import { presetFor, schedulePresets } from "../../lib/cron";
import { formatDateUTC, relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { InlineNumber } from "../settings/fields";
import form from "../settings/Form.module.css";
import styles from "./PolicyCard.module.css";

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

  const reset = () => {
    setDraft(saved);
    setCustom(presetFor(saved.schedule) === "custom");
    save.reset();
  };

  return (
    <form
      className={styles.policy}
      onSubmit={(e) => {
        e.preventDefault();
        if (!dirty || problem) return;
        save.mutate(
          { ...draft, schedule: draft.schedule.trim() },
          { onSuccess: () => toast.add({ type: "success", title: "Backup policy saved" }) },
        );
      }}
    >
      <div className={styles.head}>
        <h3 className={styles.title}>Policy</h3>
        {dirty && (
          <div className={styles.actions}>
            <Button size="sm" type="button" disabled={save.isPending} onClick={reset}>
              Discard
            </Button>
            <Button
              size="sm"
              type="submit"
              variant="primary"
              loading={save.isPending}
              disabled={problem !== null}
            >
              Save
            </Button>
          </div>
        )}
      </div>
      <Rows>
        <SettingRow
          label={<HelpTip topic="backupSchedule">Scheduled backups</HelpTip>}
          description="Back up automatically on a schedule."
          control={
            <Switch
              checked={draft.enabled}
              onCheckedChange={(v) => set("enabled", v)}
              aria-label="Scheduled backups"
            />
          }
        >
          {draft.enabled && (
            <Well>
              <div className={styles.nested}>
                <div className={styles.nestedText}>
                  <span className={styles.nestedLabel}>Schedule</span>
                  <span className={styles.hint}>
                    <ScheduleHint policy={policy} draft={draft} custom={mode === "custom"} />
                  </span>
                </div>
                <div className={styles.schedule}>
                  {mode === "custom" && (
                    <InputGroup size="sm" className={styles.cron}>
                      <InputGroup.Input
                        aria-label="Cron schedule"
                        value={draft.schedule}
                        onChange={(e) => set("schedule", e.target.value)}
                        placeholder="0 3 * * *"
                        autoComplete="off"
                        spellCheck={false}
                      />
                    </InputGroup>
                  )}
                  <Tabs
                    variant="segmented"
                    size="sm"
                    className={styles.presets}
                    value={mode}
                    onValueChange={(v) => pickMode(v as ScheduleMode)}
                    tabs={[
                      ...schedulePresets.map((p) => ({
                        value: p.id,
                        label: <span title={p.title}>{p.label}</span>,
                      })),
                      { value: "custom", label: "Custom" },
                    ]}
                  />
                </div>
              </div>
            </Well>
          )}
        </SettingRow>
        <SettingRow
          label="Compression"
          description={compressionOptions.find((o) => o.value === draft.compression)?.hint}
          control={
            <Select
              aria-label="Compression"
              className={styles.select}
              value={draft.compression}
              onValueChange={(v) => set("compression", v as BackupCompression)}
              items={compressionOptions.map((o) => ({ value: o.value, label: o.label }))}
            />
          }
        />
        <SettingRow
          label="Upload to S3"
          description={
            s3Configured ? (
              "Copy each backup to the S3 destination."
            ) : (
              <>
                No S3 destination is configured. Add one in{" "}
                <Link to="/server/$section" params={{ section: "backups" }} className={styles.link}>
                  settings
                </Link>
                .
              </>
            )
          }
          control={
            <Switch
              checked={uploads}
              disabled={!s3Configured}
              onCheckedChange={(v) => set("upload", v)}
              aria-label="Upload to S3"
            />
          }
        />
        <SettingRow
          label={<HelpTip topic="backupRetention">Retention</HelpTip>}
          description="Older scheduled backups are pruned. Manual backups stay until you delete them."
          control={
            <div className={styles.retention}>
              <span className={form.sentence}>
                <span>Keep the last</span>
                <InlineNumber
                  aria-label="Local backups to keep"
                  min={0}
                  step={1}
                  value={draft.keepLocal}
                  onChange={(e) => set("keepLocal", e.target.valueAsNumber)}
                />
                <span>on this server</span>
              </span>
              {uploads && (
                <span className={form.sentence}>
                  <span>and the last</span>
                  <InlineNumber
                    aria-label="Backups to keep in S3"
                    min={1}
                    step={1}
                    value={draft.keepRemote}
                    onChange={(e) => set("keepRemote", e.target.valueAsNumber)}
                  />
                  <span>in S3</span>
                </span>
              )}
            </div>
          }
        />
      </Rows>
      {dirty && problem && <p className={styles.error}>{problem}</p>}
      {save.error && <p className={styles.error}>{errorMessage(save.error)}</p>}
    </form>
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
  const next =
    unchanged && policy.nextRunAt
      ? `Next run ${relativeTime(policy.nextRunAt)}, ${formatDateUTC(policy.nextRunAt)}.`
      : "Save to see the next run.";
  return custom ? (
    <>
      5-field cron in UTC, or start with <code>CRON_TZ=&lt;zone&gt;</code>. {next}
    </>
  ) : (
    next
  );
}
