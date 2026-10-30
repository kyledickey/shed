import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { ScrollText } from "lucide-react";
import { useState } from "react";
import { useLogStream } from "../../../../../../api/events";
import { serviceQuery } from "../../../../../../api/services";
import { telemetryQuery } from "../../../../../../api/telemetry";
import type { Service } from "../../../../../../api/types";
import { Button, buttonClass } from "../../../../../../components/Button";
import { Card } from "../../../../../../components/Card";
import { FillHeight } from "../../../../../../components/logs/FillHeight";
import { RuntimeLogView } from "../../../../../../components/logs/RuntimeLogView";
import { Segmented } from "../../../../../../components/Form";
import { Stack } from "../../../../../../components/Layout";
import { EmptyState } from "../../../../../../components/Misc";
import { LogHistory } from "../../../../../../features/logs/LogHistory";
import { HelpTip } from "../../../../../../features/docs/HelpTip";

export const Route = createFileRoute("/_app/projects/$projectId/services/$serviceId/logs")({
  component: LogsPage,
});

function LogsPage() {
  const { serviceId } = Route.useParams();
  const { data: service } = useSuspenseQuery(serviceQuery(serviceId));
  const [attempt, setAttempt] = useState(0);
  const [mode, setMode] = useState<"live" | "history">("live");
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
      <Segmented
        size="sm"
        label="Log mode"
        value={mode}
        onChange={setMode}
        options={[
          { value: "live", label: "Live" },
          { value: "history", label: "History" },
        ]}
      />
      {mode === "live" ? live : <LogHistory serviceId={service.id} name={service.name} />}
    </Stack>
  );
}

function RuntimeLogs({ service, onReconnect }: { service: Service; onReconnect: () => void }) {
  const { lines, state, clear } = useLogStream(`/api/services/${service.id}/logs`);
  const stopped = service.status === "offline" || service.status === "failed";

  if (lines.length === 0 && (stopped || state === "ended")) {
    return (
      <Card>
        <NotRunning service={service} onReconnect={onReconnect} />
      </Card>
    );
  }

  return (
    <FillHeight>
      {(height) => (
        <RuntimeLogView
          lines={lines}
          state={state}
          title={
            <>
              {service.name} logs <HelpTip topic="runtimeLogs" />
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
        <EmptyState
          icon={<ScrollText />}
          tone="neutral"
          title="Nothing is running"
          description="Deploy this service to see its logs. They stream here live while the container runs."
        />
      );
    case "failed":
      return (
        <EmptyState
          icon={<ScrollText />}
          tone="neutral"
          title="Nothing is running"
          description="The last deployment failed, so there's no container to read logs from."
          actions={
            latest && (
              <Link
                to="/projects/$projectId/services/$serviceId"
                params={{ projectId: service.projectId, serviceId: service.id }}
                search={{ deployment: latest.id }}
                className={buttonClass()}
              >
                View build logs
              </Link>
            )
          }
        />
      );
    case "deploying":
      return (
        <EmptyState
          icon={<ScrollText />}
          tone="neutral"
          title="Waiting for the container"
          description="Logs stream here as soon as the deployment starts its container."
        />
      );
    default:
      return (
        <EmptyState
          icon={<ScrollText />}
          tone="neutral"
          title="The container isn't running"
          description="The log stream ended without output."
          actions={<Button onClick={onReconnect}>Reconnect</Button>}
        />
      );
  }
}
