import { Switch } from "@cloudflare/kumo";
import { InfoIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useSaveNotificationRules } from "../../api/notifications";
import type { NotificationChannel, NotificationRule, NotificationRulePatch } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { Pill } from "../../components/Pill";
import { SettingRow, Well } from "../../components/Settings";
import { InlineNumber } from "../settings/fields";
import form from "../settings/Form.module.css";
import { Group } from "../settings/Group";
import { SettingsForm } from "../settings/SectionForm";
import styles from "./Notifications.module.css";

type Entry = { enabled: boolean; threshold: string; minutes: string };

/** Drafts hold only the rules the user touched. */
type Draft = Record<string, Entry | undefined>;

const savedEntry = (r: NotificationRule): Entry => ({
  enabled: r.enabled,
  threshold: r.threshold?.toString() ?? "",
  minutes: r.forSeconds === null ? "" : (r.forSeconds / 60).toString(),
});

const validThreshold = (s: string) =>
  Number.isFinite(Number(s)) && Number(s) > 0 && s.trim() !== "";

/** comparator is the word before a threshold: a count is reached, a level is exceeded. */
const comparator = (unit: string) => (unit === "%" || unit === "s" ? "Above" : "At");

const validMinutes = (s: string) => Number.isFinite(Number(s)) && Number(s) >= 0 && s.trim() !== "";

/** RulesCard turns notification rules on and off and sets their thresholds and durations. */
export function RulesCard({
  rules,
  channels,
}: {
  rules: NotificationRule[];
  channels: NotificationChannel[];
}) {
  const [draft, setDraft] = useState<Draft>({});
  const entry = (r: NotificationRule) => draft[r.id] ?? savedEntry(r);
  const save = useSaveNotificationRules();
  const toast = useToast();

  const patches = rules.flatMap((r) => {
    const d = entry(r);
    const patch: NotificationRulePatch = {};
    if (d.enabled !== r.enabled) patch.enabled = d.enabled;
    if (r.threshold !== null && d.threshold !== savedEntry(r).threshold) {
      patch.threshold = Number(d.threshold);
    }
    if (r.forSeconds !== null && d.minutes !== savedEntry(r).minutes) {
      patch.forSeconds = Math.round(Number(d.minutes) * 60);
    }
    return Object.keys(patch).length > 0 ? [{ id: r.id, patch }] : [];
  });
  const invalid = rules.some(
    (r) =>
      (r.threshold !== null && !validThreshold(entry(r).threshold)) ||
      (r.forSeconds !== null && !validMinutes(entry(r).minutes)),
  );

  const set = (r: NotificationRule, change: Partial<Entry>) =>
    setDraft((d) => ({ ...d, [r.id]: { ...(d[r.id] ?? savedEntry(r)), ...change } }));

  const noChannel = !channels.some((c) => c.enabled);
  const events = rules.filter((r) => !r.needsTelemetry);
  const metrics = rules.filter((r) => r.needsTelemetry);

  const row = (r: NotificationRule) => (
    <SettingRow
      key={r.id}
      label={
        <>
          {r.title}
          {!r.available && <Pill>Needs telemetry</Pill>}
        </>
      }
      description={r.description}
      control={
        r.available ? (
          <>
            {(r.threshold !== null || r.forSeconds !== null) && (
              <span className={form.sentence}>
                {r.threshold !== null && (
                  <>
                    <span>{comparator(r.unit)}</span>
                    <InlineNumber
                      aria-label={`${r.title} threshold`}
                      min={0}
                      step="any"
                      invalid={!validThreshold(entry(r).threshold)}
                      value={entry(r).threshold}
                      onChange={(e) => set(r, { threshold: e.target.value })}
                    />
                    <span>{r.unit}</span>
                  </>
                )}
                {r.forSeconds !== null && (
                  <>
                    <span>{r.threshold !== null ? "for" : "For"}</span>
                    <InlineNumber
                      aria-label={`${r.title} sustained for, in minutes`}
                      min={0}
                      max={1440}
                      step="any"
                      invalid={!validMinutes(entry(r).minutes)}
                      value={entry(r).minutes}
                      onChange={(e) => set(r, { minutes: e.target.value })}
                    />
                    <span>min</span>
                  </>
                )}
              </span>
            )}
            <Switch
              aria-label={r.title}
              checked={entry(r).enabled}
              onCheckedChange={(enabled) => set(r, { enabled })}
            />
          </>
        ) : (
          (r.threshold !== null || r.forSeconds !== null) && (
            <span className={styles.fixed}>
              {[
                r.threshold !== null &&
                  `${comparator(r.unit)} ${r.threshold}${r.unit === "%" ? "" : " "}${r.unit}`,
                r.forSeconds !== null &&
                  `${r.threshold !== null ? "for" : "For"} ${r.forSeconds / 60} min`,
              ]
                .filter(Boolean)
                .join(" ")}
            </span>
          )
        )
      }
    />
  );

  return (
    <Group
      title="Rules"
      description="What sends a notification. Every rule sends to every enabled channel."
    >
      {noChannel && (
        <Well>
          <div className={form.note}>
            <InfoIcon size={16} />
            {channels.length === 0
              ? "There are no channels, so rules have nowhere to send. Events that happen now are not saved for later."
              : "No channel is enabled, so rules have nowhere to send. Events that happen now are not saved for later."}
          </div>
        </Well>
      )}
      <SettingsForm
        form={{
          dirty: patches.length > 0,
          pending: save.isPending,
          error: save.error,
          invalid,
          message: invalid ? "Thresholds must be above 0 and durations 0 or more." : undefined,
          reset: () => {
            setDraft({});
            save.reset();
          },
          save: () =>
            save.mutate(patches, {
              onSuccess: () => toast.add({ type: "success", title: "Rules saved" }),
            }),
        }}
      >
        {events.map(row)}
        {metrics.length > 0 && <h4 className={form.subhead}>Metrics</h4>}
        {metrics.map(row)}
      </SettingsForm>
    </Group>
  );
}
