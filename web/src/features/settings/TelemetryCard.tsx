import { Banner, Switch } from "@cloudflare/kumo";
import { WarningIcon } from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
import { serviceTelemetryQuery, useSetServiceCaps } from "../../api/telemetry";
import type { Service, TelemetryCaps } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow, Well } from "../../components/Settings";
import { HelpTip } from "../docs/HelpTip";
import { NumberInput } from "./fields";
import styles from "./Telemetry.module.css";
import { SettingsForm } from "./SectionForm";

const capFields: { key: keyof TelemetryCaps; label: string; unit: string }[] = [
  { key: "spansPerSecond", label: "Spans", unit: "/s" },
  { key: "logsPerSecond", label: "Log records", unit: "/s" },
  { key: "series", label: "Series", unit: "per hour" },
];

/** TelemetrySection shows whether the service is dropping telemetry and edits its caps. */
export function TelemetrySection({ service }: { service: Service }) {
  const { data } = useSuspenseQuery(serviceTelemetryQuery(service.id));
  const save = useSetServiceCaps(service.id);
  const toast = useToast();
  const [own, setOwn] = useState(data.overridden);
  const [caps, setCaps] = useState(data.caps);
  const dirty =
    own !== data.overridden || (own && capFields.some(({ key }) => caps[key] !== data.caps[key]));
  const invalid = own && capFields.some(({ key }) => !(caps[key] >= 1));

  return (
    <>
      {data.listenerError && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title="Not receiving OTLP"
          description={data.listenerError}
        />
      )}
      {data.dropping && (
        <Banner
          variant="alert"
          icon={<WarningIcon weight="fill" />}
          title="Dropping telemetry"
          description="This service is sending more than its caps allow. Records over a cap are dropped."
        />
      )}
      <SettingsForm
        form={{
          dirty,
          invalid,
          message: invalid ? "Every cap must be at least 1." : undefined,
          pending: save.isPending,
          error: save.error,
          reset: () => {
            setOwn(data.overridden);
            setCaps(data.caps);
            save.reset();
          },
          save: () =>
            save.mutate(own ? caps : null, {
              onSuccess: (t) => {
                setCaps(t.caps);
                toast.add({
                  type: "success",
                  title: "Saved",
                  description: "Applies within seconds.",
                });
              },
            }),
        }}
      >
        <SettingRow
          label={
            <>
              <HelpTip topic="telemetryCaps">Own caps</HelpTip>
            </>
          }
          description="Limit what this service may send. Records over a cap are dropped. Off uses the defaults from Server → Telemetry."
          control={<Switch aria-label="Own caps" checked={own} onCheckedChange={setOwn} />}
        >
          <Well>
            <div className={styles.caps}>
              {capFields.map(({ key, label, unit }) => (
                <NumberInput
                  key={key}
                  aria-label={label}
                  prefix={label}
                  unit={unit}
                  min={1}
                  step={1}
                  disabled={!own}
                  className={styles.cap}
                  value={caps[key]}
                  onChange={(e) => setCaps((c) => ({ ...c, [key]: Number(e.target.value) }))}
                />
              ))}
            </div>
          </Well>
        </SettingRow>
      </SettingsForm>
    </>
  );
}
