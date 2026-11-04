import { Button, buttonVariants } from "@cloudflare/kumo";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { useLogStream } from "../../../../../../api/events";
import { serviceQuery } from "../../../../../../api/services";
import { telemetryQuery } from "../../../../../../api/telemetry";
import type { Service } from "../../../../../../api/types";
import { FillHeight } from "../../../../../../components/logs/FillHeight";
import { RuntimeLogView } from "../../../../../../components/logs/RuntimeLogView";
import { Stack } from "../../../../../../components/Layout";
import { Notice } from "../../../../../../components/Notice";
import { LogHistory, LogModeTabs, type LogMode } from "../../../../../../features/logs/LogHistory";
import { HelpTip } from "../../../../../../features/docs/HelpTip";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/logs")({
  component: LogsPage,
});

function LogsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const [attempt, setAttempt] = useState(0);
  const [mode, setMode] = useState<LogMode>("live");
  const { data: telemetry } = useQuery(telemetryQuery);
  const historyOn = telemetry?.settings.enabled === true;
  // The stream ends when the container stops, so reconnect whenever the
  // service status changes (e.g. a new deployment goes live).
  const live = (
    <RuntimeLogs
      key={`${service.id}-${service.status}-${attempt}`}
      service={service}
      onReconnect={() => setAttempt((n) => n + 1)}
    />
  );
  if (!historyOn) return live;
  return (
    <Stack gap={3}>
      <LogModeTabs mode={mode} onChange={setMode} />
      {mode === "live" ? live : <LogHistory serviceId={service.id} name={service.name} />}
    </Stack>
  );
}

function RuntimeLogs({ service, onReconnect }: { service: Service; onReconnect: () => void }) {
  const { lines, state, clear } = useLogStream(`/api/services/${service.id}/logs`);
  const stopped = service.status === "offline" || service.status === "failed";

  if (lines.length === 0 && (stopped || state === "ended")) {
    return <NotRunning service={service} onReconnect={onReconnect} />;
  }

  return (
    <FillHeight>
      {(height) => (
        <RuntimeLogView
          lines={lines}
          state={state}
          title={
            <>
              <HelpTip topic="runtimeLogs">{service.name} logs</HelpTip>
            </>
          }
          onClear={clear}
          height={height}
        />
      )}
    </FillHeight>
  );
}

function NotRunning({ service, onReconnect }: { service: Service; onReconnect: () => void }) {
  const latest = service.latestDeployment;
  switch (service.status) {
    case "offline":
      return (
        <Notice title="Nothing is running">
          Deploy this service to see its logs. They stream here live while the container runs.
        </Notice>
      );
    case "failed":
      return (
        <Notice
          title="Nothing is running"
          actions={
            latest && (
              <Link
                className={buttonVariants({ size: "sm" })}
                to="/projects/$projectId/services/$serviceId"
                params={{ projectId: service.projectId, serviceId: service.id }}
                search={{ deployment: latest.id }}
              >
                View build logs
              </Link>
            )
          }
        >
          The last deployment failed, so there's no container to read logs from.
        </Notice>
      );
    case "deploying":
      return (
        <Notice title="Waiting for the container">
          Logs stream here as soon as the deployment starts its container.
        </Notice>
      );
    default:
      return (
        <Notice
          title="The container isn't running"
          actions={
            <Button size="sm" onClick={onReconnect}>
              Reconnect
            </Button>
          }
        >
          The log stream ended without output.
        </Notice>
      );
  }
}
