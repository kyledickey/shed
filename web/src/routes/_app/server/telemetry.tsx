import { createFileRoute } from "@tanstack/react-router";
import { telemetryQuery } from "../../../api/telemetry";
import { TelemetryPage } from "../../../features/telemetry/TelemetryPage";

export const Route = createFileRoute("/_app/server/telemetry")({
  loader: ({ context: { queryClient } }) => queryClient.ensureQueryData(telemetryQuery),
  component: TelemetryPage,
});
