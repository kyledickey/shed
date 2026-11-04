import type { QueryClient } from "@tanstack/react-query";
import { useSuspenseQuery } from "@tanstack/react-query";
import type { ComponentType } from "react";
import { auditSettingsQuery } from "../../api/audit";
import { backupSettingsQuery, systemBackupsQuery } from "../../api/backups";
import type { MetricsRange } from "../../api/types";
import {
  notificationChannelsQuery,
  notificationDeliveriesQuery,
  notificationProvidersQuery,
  notificationRulesQuery,
} from "../../api/notifications";
import { onePasswordQuery } from "../../api/onepassword";
import { telemetryQuery } from "../../api/telemetry";
import { AuditLog } from "../audit/AuditLog";
import { EncryptionCard } from "../backups/EncryptionCard";
import { S3Card } from "../backups/S3Card";
import { SystemBackups } from "../backups/SystemBackups";
import { Notifications } from "../notifications/Notifications";
import { AgentsCard } from "../settings/AgentsCard";
import { Group, Groups } from "../settings/Group";
import { OnePasswordCard } from "../settings/OnePasswordCard";
import { UpdatesCard } from "../settings/UpdatesCard";
import { TelemetryPage } from "../telemetry/TelemetryPage";
import { HelpTip } from "../docs/HelpTip";
import { HostMetrics } from "./HostMetrics";
import { ShedLogs } from "./ShedLogs";

/** SectionProps carries the URL state a section may keep, such as a chart range. */
export type SectionProps = { range: MetricsRange; setRange: (range: MetricsRange) => void };

/**
 * ServerSection is one area of the machine and of shed itself. Monitor
 * sections show what is happening; configure sections change how shed runs.
 */
export type ServerSection = {
  id: string;
  title: string;
  description: string;
  group: "monitor" | "configure";
  load?: (queryClient: QueryClient) => Promise<unknown>;
  Component: ComponentType<SectionProps>;
};

export const serverSections: ServerSection[] = [
  {
    id: "metrics",
    title: "Metrics",
    description: "CPU, memory, disk, and network for the whole machine.",
    group: "monitor",
    Component: HostMetrics,
  },
  {
    id: "logs",
    title: "Logs",
    description: "shed's own log, live.",
    group: "monitor",
    Component: ShedLogs,
  },
  {
    id: "audit",
    title: "Audit log",
    description: "Who changed what, and when.",
    group: "monitor",
    load: (qc) => qc.ensureQueryData(auditSettingsQuery),
    Component: AuditLog,
  },
  {
    id: "updates",
    title: "Updates",
    description: "Release channel and installing new versions of shed.",
    group: "configure",
    Component: UpdatesCard,
  },
  {
    id: "backups",
    title: "Backups",
    description: "Where backups go, how they are encrypted, and shed's own database.",
    group: "configure",
    load: (qc) =>
      Promise.all([
        qc.ensureQueryData(backupSettingsQuery),
        qc.ensureQueryData(systemBackupsQuery),
      ]),
    Component: BackupsSection,
  },
  {
    id: "notifications",
    title: "Notifications",
    description: "Channels, the events that reach them, and recent deliveries.",
    group: "configure",
    load: (qc) =>
      Promise.all([
        qc.ensureQueryData(notificationProvidersQuery),
        qc.ensureQueryData(notificationChannelsQuery),
        qc.ensureQueryData(notificationRulesQuery),
        qc.ensureQueryData(notificationDeliveriesQuery),
      ]),
    Component: Notifications,
  },
  {
    id: "telemetry",
    title: "Telemetry",
    description: "Grafana, metrics, logs, and traces for your services.",
    group: "configure",
    load: (qc) => qc.ensureQueryData(telemetryQuery),
    Component: TelemetryPage,
  },
  {
    id: "integrations",
    title: "Integrations",
    description: "Agents that use shed over MCP, and 1Password for secrets.",
    group: "configure",
    load: (qc) => qc.ensureQueryData(onePasswordQuery),
    Component: IntegrationsSection,
  },
];

export function findSection(id: string | undefined): ServerSection | undefined {
  return serverSections.find((s) => s.id === id);
}

function BackupsSection() {
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);
  return (
    <Groups>
      <Group
        title={<HelpTip topic="backupS3">S3 destination</HelpTip>}
        description="Where backups are uploaded, besides this server."
      >
        <S3Card settings={settings} />
      </Group>
      <Group title="Encryption" description="Protect archives with age.">
        <EncryptionCard settings={settings} />
      </Group>
      <Group
        title="shed database"
        description="shed.db holds projects, services, and settings. Snapshots follow their own schedule."
      >
        <SystemBackups />
      </Group>
    </Groups>
  );
}

function IntegrationsSection() {
  const { data: onePassword } = useSuspenseQuery(onePasswordQuery);
  return (
    <Groups>
      <AgentsCard />
      <OnePasswordCard status={onePassword} />
    </Groups>
  );
}
