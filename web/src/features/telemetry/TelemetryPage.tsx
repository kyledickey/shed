import { useSuspenseQuery } from "@tanstack/react-query";
import { BellRing, ExternalLink, Info, RotateCw } from "lucide-react";
import { useState, type ReactNode } from "react";
import { errorMessage } from "../../api/client";
import {
  telemetryQuery,
  useDeleteTelemetryData,
  useRestartTelemetryComponent,
  useRestoreDashboards,
  useSaveTelemetry,
} from "../../api/telemetry";
import type {
  Telemetry,
  TelemetryComponent,
  TelemetryComponentName,
  TelemetrySettings,
} from "../../api/types";
import { Badge, Spinner } from "../../components/Badge";
import { Button, LinkButton } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input, Switch } from "../../components/Form";
import { Section, Stack } from "../../components/Layout";
import { Callout, CopyButton } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import type { Tone } from "../../components/tone";
import { formatBytes, formatPercent } from "../../lib/format";
import { relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { SettingsCard } from "../settings/SectionForm";
import settingsStyles from "../settings/Settings.module.css";
import styles from "./Telemetry.module.css";

const componentLabels: Record<TelemetryComponentName, string> = {
  metrics: "VictoriaMetrics",
  logs: "VictoriaLogs",
  traces: "VictoriaTraces",
  grafana: "Grafana",
};

const stateLooks: Record<
  TelemetryComponent["state"],
  { tone: Tone; label: string; live?: boolean }
> = {
  running: { tone: "grass", label: "Running" },
  starting: { tone: "sky", label: "Starting", live: true },
  stopped: { tone: "neutral", label: "Stopped" },
  failed: { tone: "tomato", label: "Failed" },
};

/** TelemetryPage turns the telemetry stack on and off, tunes it, and shows how it is doing. */
export function TelemetryPage() {
  const { data } = useSuspenseQuery(telemetryQuery);
  return (
    <Section
      title="Telemetry"
      description="Metrics, logs, and traces of every service, kept on this server and explored in Grafana."
    >
      <Stack gap={4}>
        <EnableCard telemetry={data} />
        {data.components.length > 0 && <ComponentsCard telemetry={data} />}
        {data.settings.enabled && <IngestCard telemetry={data} />}
        {data.settings.enabled && <DashboardsCard />}
        <SettingsForm settings={data.settings} />
        <DeleteCard telemetry={data} />
      </Stack>
    </Section>
  );
}

function EnableCard({ telemetry }: { telemetry: Telemetry }) {
  const save = useSaveTelemetry();
  const [toggled, setToggled] = useState(false);
  const { settings } = telemetry;
  return (
    <LayerCard
      title="Telemetry stack"
      actions={
        settings.enabled && (
          <LinkButton size="sm" href={telemetry.grafanaUrl} target="_blank" rel="noreferrer">
            <ExternalLink size={14} />
            Open Grafana
          </LinkButton>
        )
      }
      padded
    >
      <div className={settingsStyles.fields}>
        <Switch
          checked={settings.enabled}
          disabled={save.isPending}
          onChange={(enabled) =>
            save.mutate({ ...settings, enabled }, { onSuccess: () => setToggled(true) })
          }
          label={
            <>
              Collect telemetry <HelpTip topic="telemetry" />
            </>
          }
          description="Runs VictoriaMetrics, VictoriaLogs, VictoriaTraces, and Grafana as containers on this server. Turning it off stops them and keeps their data."
        />
        <dl className={settingsStyles.facts}>
          <div>
            <dt>OTLP endpoint for services</dt>
            <dd>
              <code className={styles.endpoint}>{telemetry.otlpEndpoint}</code>
              <CopyButton value={telemetry.otlpEndpoint} label="Copy OTLP endpoint" />
            </dd>
          </div>
          <div>
            <dt>Grafana</dt>
            <dd>
              <code className={styles.endpoint}>{telemetry.grafanaUrl}</code>
            </dd>
          </div>
        </dl>
        {save.error && <p className={settingsStyles.error}>{errorMessage(save.error)}</p>}
        {toggled && (
          <Callout tone="sky" icon={<Info size={16} />}>
            Apps get the OTLP variables, or lose them, with their next deployment.
          </Callout>
        )}
      </div>
    </LayerCard>
  );
}

function ComponentsCard({ telemetry }: { telemetry: Telemetry }) {
  const restart = useRestartTelemetryComponent();
  const toast = useToast();
  const enabled = telemetry.settings.enabled;
  return (
    <LayerCard title="Components">
      <table className={styles.table}>
        <thead>
          <tr>
            <th>Component</th>
            <th>State</th>
            <th className={styles.num}>CPU</th>
            <th className={styles.num}>Memory</th>
            <th className={styles.num}>Disk</th>
            <th aria-label="Actions" />
          </tr>
        </thead>
        <tbody>
          {telemetry.components.map((c) => {
            const look = stateLooks[c.state];
            return (
              <tr key={c.name}>
                <td>
                  <span className={styles.name}>{componentLabels[c.name]}</span>
                  <span className={styles.version}>{c.version}</span>
                  {c.error && <span className={styles.problem}>{c.error}</span>}
                </td>
                <td>
                  <Badge tone={look.tone} size="sm">
                    {look.live && <Spinner tone={look.tone} size={8} />}
                    {look.label}
                  </Badge>
                </td>
                <td className={styles.num}>
                  {c.cpuPercent === null ? "—" : formatPercent(c.cpuPercent)}
                </td>
                <td className={styles.num}>
                  {c.memoryBytes === null ? "—" : formatBytes(c.memoryBytes)}
                  <span className={styles.limit}> / {formatBytes(c.memoryLimit)}</span>
                </td>
                <td className={styles.num}>
                  {c.diskBytes === null ? "—" : formatBytes(c.diskBytes)}
                </td>
                <td className={styles.actions}>
                  {enabled && (
                    <Button
                      variant="ghost"
                      size="sm"
                      icon
                      aria-label={`Restart ${componentLabels[c.name]}`}
                      loading={restart.isPending && restart.variables === c.name}
                      onClick={() =>
                        restart.mutate(c.name, {
                          onSuccess: () =>
                            toast.add({
                              type: "success",
                              title: `${componentLabels[c.name]} restarted`,
                            }),
                          onError: (err) =>
                            toast.add({
                              type: "error",
                              title: "Restart failed",
                              description: errorMessage(err),
                            }),
                        })
                      }
                    >
                      <RotateCw size={14} />
                    </Button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </LayerCard>
  );
}

function IngestCard({ telemetry }: { telemetry: Telemetry }) {
  const { ingest, droppedLastHour, firingAlerts } = telemetry;
  const rate = (n: number) => `${Number(n.toFixed(n < 10 ? 1 : 0))}/s`;
  return (
    <LayerCard title="Ingest" meta="Last minute" padded>
      <div className={settingsStyles.fields}>
        <dl className={settingsStyles.facts}>
          <Fact label="Spans">{rate(ingest.spans)}</Fact>
          <Fact label="Log records">{rate(ingest.logs)}</Fact>
          <Fact label="Metric points">{rate(ingest.points)}</Fact>
          <Fact label="Dropped in the last hour">
            {droppedLastHour.reduce((sum, d) => sum + d.count, 0).toFixed(0)}
          </Fact>
        </dl>
        {droppedLastHour.length > 0 && (
          <ul className={styles.drops}>
            {droppedLastHour.map((d) => (
              <li key={`${d.signal}/${d.reason}`}>
                <Badge size="sm" variant="outline">
                  {d.signal}
                </Badge>{" "}
                {d.reason.replaceAll("_", " ")}: {d.count.toFixed(0)}
              </li>
            ))}
          </ul>
        )}
        {firingAlerts.map((a) => (
          <Callout
            key={`${a.name}/${a.serviceId}`}
            tone="tomato"
            icon={<BellRing size={16} />}
            title={a.name}
          >
            Firing since {relativeTime(a.since)}.
          </Callout>
        ))}
      </div>
    </LayerCard>
  );
}

function DashboardsCard() {
  const restore = useRestoreDashboards();
  const toast = useToast();
  const onRestore = () =>
    restore.mutate(undefined, {
      onSuccess: () => toast.add({ title: "Default dashboards restored", type: "info" }),
      onError: (err) =>
        toast.add({ title: "Couldn't restore", description: errorMessage(err), type: "error" }),
    });
  return (
    <LayerCard
      title={
        <>
          Dashboards <HelpTip topic="dashboards" />
        </>
      }
    >
      <div className={settingsStyles.section}>
        <div className={settingsStyles.danger}>
          <p className={settingsStyles.note}>
            shed keeps default dashboards for itself, each project, and each service. Ones you save
            in Grafana are left alone, and ones you delete stay deleted until you restore them.
          </p>
          <Button loading={restore.isPending} onClick={onRestore}>
            <RotateCw size={14} />
            Restore default dashboards
          </Button>
        </div>
      </div>
    </LayerCard>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** validateSettings returns the first problem with draft settings, or null when they can be saved. */
export function validateSettings(s: TelemetrySettings): string | null {
  const days = [s.metricsRetentionDays, s.logsRetentionDays, s.tracesRetentionDays];
  if (days.some((d) => !Number.isInteger(d) || d < 1 || d > 3650)) {
    return "Retention must be 1 to 3650 days.";
  }
  if (!Number.isInteger(s.diskLimitGB) || s.diskLimitGB < 1)
    return "The disk limit must be at least 1 GB.";
  const caps = Object.values(s.defaultCaps);
  if (caps.some((c) => !Number.isInteger(c) || c < 1)) return "Every cap must be at least 1.";
  return null;
}

function SettingsForm({ settings }: { settings: TelemetrySettings }) {
  const [draft, setDraft] = useState(settings);
  const save = useSaveTelemetry();
  const toast = useToast();
  const set = <K extends keyof TelemetrySettings>(key: K, value: TelemetrySettings[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));
  const setCap = (key: keyof TelemetrySettings["defaultCaps"], value: number) =>
    setDraft((d) => ({ ...d, defaultCaps: { ...d.defaultCaps, [key]: value } }));
  const { enabled: _, ...saved } = settings;
  const { enabled: __, ...edited } = draft;
  const dirty = JSON.stringify(saved) !== JSON.stringify(edited);
  const problem = validateSettings(draft);

  const number = (value: number, onChange: (n: number) => void, suffix: string) => (id: string) => (
    <Input
      id={id}
      type="number"
      min={1}
      step={1}
      mono
      suffix={suffix}
      value={Number.isNaN(value) ? "" : value}
      onChange={(e) => onChange(e.target.valueAsNumber)}
    />
  );

  return (
    <SettingsCard
      title="Retention and limits"
      meta="Changing a retention restarts that store; data is kept."
      form={{
        dirty,
        invalid: problem !== null,
        pending: save.isPending,
        error: save.error,
        reset: () => {
          setDraft(settings);
          save.reset();
        },
        save: () =>
          save.mutate(
            { ...draft, enabled: settings.enabled },
            { onSuccess: () => toast.add({ type: "success", title: "Telemetry settings saved" }) },
          ),
      }}
    >
      <div className={settingsStyles.twoCol}>
        <Field label="Keep metrics for">
          {number(draft.metricsRetentionDays, (n) => set("metricsRetentionDays", n), "days")}
        </Field>
        <Field label="Keep logs for">
          {number(draft.logsRetentionDays, (n) => set("logsRetentionDays", n), "days")}
        </Field>
        <Field label="Keep traces for">
          {number(draft.tracesRetentionDays, (n) => set("tracesRetentionDays", n), "days")}
        </Field>
        <Field
          label={
            <>
              Disk for logs and traces <HelpTip topic="telemetryDisk" />
            </>
          }
          hint="60% for logs, 40% for traces; the oldest data goes first."
        >
          {number(draft.diskLimitGB, (n) => set("diskLimitGB", n), "GB")}
        </Field>
      </div>
      <p className={settingsStyles.subhead}>Default limits per service</p>
      <div className={settingsStyles.twoCol}>
        <Field label="Spans">
          {number(
            draft.defaultCaps.spansPerSecond,
            (n) => setCap("spansPerSecond", n),
            "per second",
          )}
        </Field>
        <Field label="Log records">
          {number(draft.defaultCaps.logsPerSecond, (n) => setCap("logsPerSecond", n), "per second")}
        </Field>
        <Field label="Metric series" hint="Distinct series in the last hour.">
          {number(draft.defaultCaps.series, (n) => setCap("series", n), "series")}
        </Field>
      </div>
      {dirty && problem && <p className={settingsStyles.error}>{problem}</p>}
    </SettingsCard>
  );
}

function DeleteCard({ telemetry }: { telemetry: Telemetry }) {
  const [confirming, setConfirming] = useState(false);
  const remove = useDeleteTelemetryData();
  const toast = useToast();
  const enabled = telemetry.settings.enabled;
  return (
    <LayerCard title="Danger zone">
      <div className={settingsStyles.section}>
        <div className={settingsStyles.danger}>
          <p className={settingsStyles.note}>
            {enabled
              ? "Turn telemetry off to delete its data."
              : "Delete the telemetry containers, all stored metrics, logs, and traces, and Grafana's dashboards."}
          </p>
          <Button variant="danger" disabled={enabled} onClick={() => setConfirming(true)}>
            Delete telemetry data
          </Button>
        </div>
      </div>
      <ConfirmDialog
        open={confirming}
        onOpenChange={(open) => {
          setConfirming(open);
          remove.reset();
        }}
        title="Delete telemetry data"
        confirmLabel="Delete data"
        typeToConfirm="delete"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          remove.mutate(undefined, {
            onSuccess: () => {
              setConfirming(false);
              toast.add({ type: "success", title: "Telemetry data deleted" });
            },
          })
        }
      >
        Every metric, log record, and trace on this server is deleted, with Grafana's users,
        dashboards, and settings. This can't be undone.
      </ConfirmDialog>
    </LayerCard>
  );
}
