import { createFileRoute } from "@tanstack/react-router";
import { useLogStream } from "../../../api/events";
import { FillHeight } from "../../../components/logs/FillHeight";
import { RuntimeLogView } from "../../../components/logs/RuntimeLogView";

export const Route = createFileRoute("/_app/server/logs")({
  component: LogsPage,
});

function LogsPage() {
  const { lines, state, clear } = useLogStream("/api/logs");
  return (
    <FillHeight>
      {(height) => (
        <RuntimeLogView
          lines={lines}
          state={state}
          title="shed logs"
          onClear={clear}
          height={height}
        />
      )}
    </FillHeight>
  );
}
