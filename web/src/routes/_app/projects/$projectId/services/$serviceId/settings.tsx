import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { useMemo, type ReactNode } from "react";
import { serviceQuery } from "../../../../../../api/services";
import type { Service } from "../../../../../../api/types";
import { SettingsLayout, type SettingsSection } from "../../../../../../components/Settings";
import {
  BuildSection,
  DeploySection,
  SourceSection,
} from "../../../../../../features/settings/AppCards";
import { DangerSection } from "../../../../../../features/settings/DangerCard";
import { NetworkingSection } from "../../../../../../features/settings/NetworkingCard";
import { ResourcesSection } from "../../../../../../features/settings/ResourcesCard";
import { TelemetrySection } from "../../../../../../features/settings/TelemetryCard";
import { VolumesSection } from "../../../../../../features/settings/VolumesCard";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/settings")({
  component: ServiceSettingsPage,
});

function ServiceSettingsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const sections = useMemo(() => settingsSections(service), [service]);
  return <SettingsLayout key={service.id} sections={sections} />;
}

function settingsSections(service: Service): SettingsSection[] {
  const isApp = service.kind === "app";
  const section = (id: string, title: string, content: ReactNode, danger?: boolean) => ({
    id,
    title,
    content,
    danger,
  });
  return [
    section("source", "Source", <SourceSection service={service} />),
    ...(isApp
      ? [
          section("build", service.repo ? "Build" : "Run", <BuildSection service={service} />),
          section("deploy", "Deploy", <DeploySection service={service} />),
        ]
      : []),
    section("resources", "Resources", <ResourcesSection service={service} />),
    section("networking", "Networking", <NetworkingSection service={service} />),
    section("volumes", "Volumes", <VolumesSection service={service} />),
    section("telemetry", "Telemetry", <TelemetrySection service={service} />),
    section("danger", "Danger zone", <DangerSection service={service} />, true),
  ];
}
