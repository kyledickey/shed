import { Info } from "lucide-react";
import { useState } from "react";
import { useSaveNotificationRules } from "../../api/notifications";
import type { NotificationChannel, NotificationRule, NotificationRulePatch } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Field, Input, Switch } from "../../components/Form";
import { Callout } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { SettingsCard } from "../settings/SectionForm";
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
  const groups = [
    { title: "Events", rules: rules.filter((r) => !r.needsTelemetry) },
    { title: "Metrics", rules: rules.filter((r) => r.needsTelemetry) },
  ];

  return (
    <SettingsCard
      title="Rules"
      meta="What sends a notification. Every rule sends to every enabled channel."
      form={{
        dirty: patches.length > 0,
        pending: save.isPending,
        error: save.error,
        invalid,
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
      {noChannel && (
        <Callout tone="sky" icon={<Info size={16} />}>
          {channels.length === 0
            ? "There are no channels, so rules have nowhere to send. Events that happen now are not saved for later."
            : "No channel is enabled, so rules have nowhere to send. Events that happen now are not saved for later."}
        </Callout>
      )}
      {groups.map((g) => (
        <section key={g.title}>
          <h3 className={styles.ruleGroup}>{g.title}</h3>
          <div>
            {g.rules.map((r) => (
              <div key={r.id} className={styles.rule}>
                <div className={styles.ruleSwitch}>
                  <Switch
                    label={
                      <>
                        {r.title}{" "}
                        {!r.available && r.needsTelemetry && (
                          <Badge size="sm" variant="outline">
                            Needs telemetry
                          </Badge>
                        )}
                      </>
                    }
                    description={r.description}
                    checked={entry(r).enabled}
                    disabled={!r.available}
                    onChange={(enabled) => set(r, { enabled })}
                  />
                </div>
                {r.threshold !== null && (
                  <div className={styles.threshold}>
                    <Field label="Threshold">
                      {(id) => (
                        <Input
                          id={id}
                          type="number"
                          inputMode="decimal"
                          min={0}
                          step="any"
                          value={entry(r).threshold}
                          onChange={(e) => set(r, { threshold: e.target.value })}
                          suffix={r.unit}
                          disabled={!r.available}
                        />
                      )}
                    </Field>
                  </div>
                )}
                {r.forSeconds !== null && (
                  <div className={styles.threshold}>
                    <Field label="For">
                      {(id) => (
                        <Input
                          id={id}
                          type="number"
                          inputMode="decimal"
                          min={0}
                          max={1440}
                          step="any"
                          value={entry(r).minutes}
                          onChange={(e) => set(r, { minutes: e.target.value })}
                          suffix="min"
                          disabled={!r.available}
                        />
                      )}
                    </Field>
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>
      ))}
    </SettingsCard>
  );
}
