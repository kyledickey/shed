import { Banner, Button, ClipboardText, LinkButton, Switch, Table } from "@cloudflare/kumo";
import {
  ArrowClockwiseIcon,
  ArrowUpRightIcon,
  BellRingingIcon,
  InfoIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useState } from "react";
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
import { Spinner } from "../../components/Badge";
import { useToast } from "../../components/Overlay";
import { Pill } from "../../components/Pill";
import { Rows, SettingRow, Well } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { formatBytes, formatPercent } from "../../lib/format";
import { relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import { InlineNumber } from "../settings/fields";
import form from "../settings/Form.module.css";
import { Group, Groups } from "../settings/Group";
import { SettingsForm } from "../settings/SectionForm";
import styles from "./Telemetry.module.css";

const componentLabels: Record<TelemetryComponentName, string> = {
  metrics: "VictoriaMetrics",
  logs: "VictoriaLogs",
  traces: "VictoriaTraces",
  grafana: "Grafana",
};

const stateLabels: Record<TelemetryComponent["state"], string> = {
  running: "Running",
  starting: "Starting",
  stopped: "Stopped",
  failed: "Failed",
};

/** TelemetryPage turns the telemetry stack on and off, tunes it, and shows how it is doing. */
export function TelemetryPage() {
  const { data } = useSuspenseQuery(telemetryQuery);
  return (
    <Groups>
      <StackGroup telemetry={data} />
      {data.components.length > 0 && <ComponentsGroup telemetry={data} />}
      {data.settings.enabled && <IngestGroup telemetry={data} />}
      {data.settings.enabled && <DashboardsGroup />}
      <SettingsGroup settings={data.settings} />
      <DeleteGroup telemetry={data} />
    </Groups>
  );
}

function StackGroup({ telemetry }: { telemetry: Telemetry }) {
  const save = useSaveTelemetry();
  const [toggled, setToggled] = useState(false);
  const { settings } = telemetry;
  return (
    <Group
      title="Telemetry stack"
      actions={
        settings.enabled && (
          <LinkButton
            size="sm"
            variant="secondary"
            href={telemetry.grafanaUrl}
            target="_blank"
            rel="noreferrer"
            icon={<ArrowUpRightIcon />}
          >
            Open Grafana
          </LinkButton>
        )
      }
    >
      <SettingRow
        label={
          <>
            <HelpTip topic="telemetry">Collect telemetry</HelpTip>
          </>
        }
        description="Runs VictoriaMetrics, VictoriaLogs, VictoriaTraces, and Grafana as containers on this server. Turning it off stops them and keeps their data."
        control={
          <Switch
            aria-label="Collect telemetry"
            checked={settings.enabled}
            disabled={save.isPending}
            onCheckedChange={(enabled) =>
              save.mutate({ ...settings, enabled }, { onSuccess: () => setToggled(true) })
            }
          />
        }
      >
        {save.error && <p className={form.error}>{errorMessage(save.error)}</p>}
        {toggled && (
          <Well>
            <div className={form.note}>
              <InfoIcon size={16} />
              Apps get the OTLP variables, or lose them, with their next deployment.
            </div>
          </Well>
        )}
      </SettingRow>
      <SettingRow
        label="OTLP endpoint"
        description="Where services send traces, metrics, and logs."
        control={
          <ClipboardText text={telemetry.otlpEndpoint} size="base" className={styles.endpoint} />
        }
      />
      <SettingRow
        label="Grafana"
        control={
          <a className={form.link} href={telemetry.grafanaUrl} target="_blank" rel="noreferrer">
            {telemetry.grafanaUrl}
            <ArrowUpRightIcon size={12} />
          </a>
        }
      />
    </Group>
  );
}

function ComponentsGroup({ telemetry }: { telemetry: Telemetry }) {
  const restart = useRestartTelemetryComponent();
  const toast = useToast();
  const enabled = telemetry.settings.enabled;
  return (
    <Group title="Components">
      <div className={form.table}>
        <Table>
          <Table.Header>
            <Table.Row>
              <Table.Head>Component</Table.Head>
              <Table.Head>State</Table.Head>
              <Table.Head className={form.cellNum}>CPU</Table.Head>
              <Table.Head className={form.cellNum}>Memory</Table.Head>
              <Table.Head className={form.cellNum}>Disk</Table.Head>
              <Table.Head aria-label="Actions" />
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {telemetry.components.map((c) => (
              <Table.Row key={c.name}>
                <Table.Cell>
                  <span className={styles.name}>{componentLabels[c.name]}</span>
                  <span className={styles.version}>{c.version}</span>
                  {c.error && <span className={form.cellProblem}>{c.error}</span>}
                </Table.Cell>
                <Table.Cell>
                  <span className={cx(form.status, c.state === "failed" && form.statusFailed)}>
                    {c.state === "starting" && <Spinner size={9} />}
                    {stateLabels[c.state]}
                  </span>
                </Table.Cell>
                <Table.Cell className={form.cellNum}>
                  {c.cpuPercent === null ? "—" : formatPercent(c.cpuPercent)}
                </Table.Cell>
                <Table.Cell className={form.cellNum}>
                  {c.memoryBytes === null ? "—" : formatBytes(c.memoryBytes)}
                  <span className={form.muted}> / {formatBytes(c.memoryLimit)}</span>
                </Table.Cell>
                <Table.Cell className={form.cellNum}>
                  {c.diskBytes === null ? "—" : formatBytes(c.diskBytes)}
                </Table.Cell>
                <Table.Cell className={form.cellActions}>
                  {enabled && (
                    <Button
                      variant="ghost"
                      size="sm"
                      shape="square"
                      icon={<ArrowClockwiseIcon />}
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
                    />
                  )}
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </div>
    </Group>
  );
}

function IngestGroup({ telemetry }: { telemetry: Telemetry }) {
  const { ingest, droppedLastHour, firingAlerts } = telemetry;
  const rate = (n: number) => `${Number(n.toFixed(n < 10 ? 1 : 0))}/s`;
  const stats = [
    { label: "Spans", value: rate(ingest.spans) },
    { label: "Log records", value: rate(ingest.logs) },
    { label: "Metric points", value: rate(ingest.points) },
    {
      label: "Dropped in the last hour",
      value: droppedLastHour.reduce((sum, d) => sum + d.count, 0).toFixed(0),
    },
  ];
  return (
    <Group title="Ingest" description="Rates over the last minute.">
      {firingAlerts.map((a) => (
        <Banner
          key={`${a.name}/${a.serviceId}`}
          variant="error"
          icon={<BellRingingIcon weight="fill" />}
          title={a.name}
          description={`Firing since ${relativeTime(a.since)}.`}
        />
      ))}
      <div className={styles.stats}>
        {stats.map((s) => (
          <div key={s.label} className={styles.stat}>
            <span className={styles.statLabel}>{s.label}</span>
            <span className={styles.statValue}>{s.value}</span>
          </div>
        ))}
      </div>
      {droppedLastHour.length > 0 && (
        <div className={form.table}>
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Signal</Table.Head>
                <Table.Head>Reason</Table.Head>
                <Table.Head className={form.cellNum}>Dropped</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {droppedLastHour.map((d) => (
                <Table.Row key={`${d.signal}/${d.reason}`}>
                  <Table.Cell>
                    <Pill>{d.signal}</Pill>
                  </Table.Cell>
                  <Table.Cell>{d.reason.replaceAll("_", " ")}</Table.Cell>
                  <Table.Cell className={form.cellNum}>{d.count.toFixed(0)}</Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </Group>
  );
}

function DashboardsGroup() {
  const restore = useRestoreDashboards();
  const toast = useToast();
  const onRestore = () =>
    restore.mutate(undefined, {
      onSuccess: () => toast.add({ title: "Default dashboards restored", type: "info" }),
      onError: (err) =>
        toast.add({ title: "Couldn't restore", description: errorMessage(err), type: "error" }),
    });
  return (
    <Group title="Dashboards">
      <SettingRow
        label={
          <>
            <HelpTip topic="dashboards">Default dashboards</HelpTip>
          </>
        }
        description="shed keeps default dashboards for itself, each project, and each service. Ones you save in Grafana are left alone, and ones you delete stay deleted until you restore them."
        control={
          <Button loading={restore.isPending} icon={<ArrowClockwiseIcon />} onClick={onRestore}>
            Restore defaults
          </Button>
        }
      />
    </Group>
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

function SettingsGroup({ settings }: { settings: TelemetrySettings }) {
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

  const number = (
    label: string,
    value: number,
    onChange: (n: number) => void,
    unit: string,
    prefix?: string,
  ) => (
    <span className={form.sentence}>
      {prefix && <span>{prefix}</span>}
      <InlineNumber
        aria-label={label}
        min={1}
        step={1}
        value={value}
        onChange={(e) => onChange(e.target.valueAsNumber)}
      />
      <span>{unit}</span>
    </span>
  );

  return (
    <Group
      title="Retention and limits"
      description="Changing a retention restarts that store; data is kept."
    >
      <SettingsForm
        form={{
          dirty,
          invalid: problem !== null,
          message: problem ?? undefined,
          pending: save.isPending,
          error: save.error,
          reset: () => {
            setDraft(settings);
            save.reset();
          },
          save: () =>
            save.mutate(
              { ...draft, enabled: settings.enabled },
              {
                onSuccess: () => toast.add({ type: "success", title: "Telemetry settings saved" }),
              },
            ),
        }}
      >
        <SettingRow
          label="Keep metrics for"
          control={number(
            "Keep metrics for",
            draft.metricsRetentionDays,
            (n) => set("metricsRetentionDays", n),
            "days",
          )}
        />
        <SettingRow
          label="Keep logs for"
          control={number(
            "Keep logs for",
            draft.logsRetentionDays,
            (n) => set("logsRetentionDays", n),
            "days",
          )}
        />
        <SettingRow
          label="Keep traces for"
          control={number(
            "Keep traces for",
            draft.tracesRetentionDays,
            (n) => set("tracesRetentionDays", n),
            "days",
          )}
        />
        <SettingRow
          label={<HelpTip topic="telemetryDisk">Disk for logs and traces</HelpTip>}
          description="60% for logs, 40% for traces; the oldest data goes first."
          control={number(
            "Disk for logs and traces",
            draft.diskLimitGB,
            (n) => set("diskLimitGB", n),
            "GB",
          )}
        />
        <SettingRow
          label="Default limits per service"
          description="What each service may send unless it sets its own caps. Series counts distinct metric series in the last hour."
        >
          <div className={styles.caps}>
            {number(
              "Spans per second",
              draft.defaultCaps.spansPerSecond,
              (n) => setCap("spansPerSecond", n),
              "/s",
              "Spans",
            )}
            {number(
              "Log records per second",
              draft.defaultCaps.logsPerSecond,
              (n) => setCap("logsPerSecond", n),
              "/s",
              "Log records",
            )}
            {number(
              "Metric series",
              draft.defaultCaps.series,
              (n) => setCap("series", n),
              "per hour",
              "Series",
            )}
          </div>
        </SettingRow>
      </SettingsForm>
    </Group>
  );
}

function DeleteGroup({ telemetry }: { telemetry: Telemetry }) {
  const [confirming, setConfirming] = useState(false);
  const remove = useDeleteTelemetryData();
  const toast = useToast();
  const enabled = telemetry.settings.enabled;
  return (
    <Group title="Danger zone">
      <Rows>
        <SettingRow
          label="Delete telemetry data"
          description={
            enabled
              ? "Turn telemetry off to delete its data."
              : "Delete the telemetry containers, all stored metrics, logs, and traces, and Grafana's dashboards."
          }
          control={
            <Button
              variant="destructive"
              icon={<TrashIcon />}
              disabled={enabled}
              onClick={() => setConfirming(true)}
            >
              Delete
            </Button>
          }
        />
      </Rows>
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
    </Group>
  );
}
