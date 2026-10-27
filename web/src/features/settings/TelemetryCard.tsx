import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
import { serviceTelemetryQuery, useSetServiceCaps } from "../../api/telemetry";
import type { Service, TelemetryCaps } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Field, Input, Switch } from "../../components/Form";
import { useToast } from "../../components/Overlay";
import { HelpTip } from "../docs/HelpTip";
import { SettingsCard } from "./SectionForm";
import styles from "./Settings.module.css";

const capFields: { key: keyof TelemetryCaps; label: string; suffix: string; hint: string }[] = [
  { key: "spansPerSecond", label: "Spans", suffix: "/s", hint: "Trace spans per second." },
  {
    key: "logsPerSecond",
    label: "Log records",
    suffix: "/s",
    hint: "OTLP log records and lines of output per second.",
  },
  {
    key: "series",
    label: "Series",
    suffix: "series",
    hint: "Distinct metric series in the last hour.",
  },
];

/** TelemetryCard shows whether the service is dropping telemetry and edits its caps. */
export function TelemetryCard({ service }: { service: Service }) {
  const { data } = useSuspenseQuery(serviceTelemetryQuery(service.id));
  const save = useSetServiceCaps(service.id);
  const toast = useToast();
  const [own, setOwn] = useState(data.overridden);
  const [caps, setCaps] = useState(data.caps);
  const dirty =
    own !== data.overridden || (own && capFields.some(({ key }) => caps[key] !== data.caps[key]));
  const invalid = own && capFields.some(({ key }) => !(caps[key] >= 1));

  return (
    <SettingsCard
      title={
        <>
          Telemetry <HelpTip topic="telemetryCaps" />{" "}
          {data.dropping && (
            <Badge tone="tangerine" size="sm">
              Dropping telemetry
            </Badge>
          )}
        </>
      }
      meta="What this service may send while telemetry is on. Records over a cap are dropped."
      form={{
        dirty,
        invalid,
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
      <div className={styles.fields}>
        <Switch
          checked={own}
          onChange={setOwn}
          label="Own caps"
          description="Off uses the defaults from Server → Telemetry."
        />
        <div className={styles.twoCol}>
          {capFields.map(({ key, label, suffix, hint }) => (
            <Field key={key} label={label} hint={hint}>
              {(id) => (
                <Input
                  id={id}
                  type="number"
                  min={1}
                  step={1}
                  mono
                  suffix={suffix}
                  disabled={!own}
                  value={caps[key]}
                  onChange={(e) => setCaps((c) => ({ ...c, [key]: Number(e.target.value) }))}
                />
              )}
            </Field>
          ))}
        </div>
      </div>
    </SettingsCard>
  );
}
