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

type Entry = { enabled: boolean; threshold: string };

/** Drafts hold only the rules the user touched. */
type Draft = Record<string, Entry | undefined>;

const savedEntry = (r: NotificationRule): Entry => ({
  enabled: r.enabled,
  threshold: r.threshold?.toString() ?? "",
});

const validThreshold = (s: string) =>
  Number.isFinite(Number(s)) && Number(s) > 0 && s.trim() !== "";

/** RulesCard turns notification rules on and off and sets their thresholds. */
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
    return Object.keys(patch).length > 0 ? [{ id: r.id, patch }] : [];
  });
  const invalid = rules.some((r) => r.threshold !== null && !validThreshold(entry(r).threshold));

  const set = (r: NotificationRule, change: Partial<Entry>) =>
    setDraft((d) => ({ ...d, [r.id]: { ...(d[r.id] ?? savedEntry(r)), ...change } }));

  const noChannel = !channels.some((c) => c.enabled);

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
      <div>
        {rules.map((r) => (
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
                <Field label={`${r.title} threshold`}>
                  {(id) => (
                    <Input
                      id={id}
                      type="number"
                      inputMode="decimal"
                      min={1}
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
          </div>
        ))}
      </div>
    </SettingsCard>
  );
}
