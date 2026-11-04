import { useLogStream } from "../../api/events";
import { FillHeight } from "../../components/logs/FillHeight";
import { RuntimeLogView } from "../../components/logs/RuntimeLogView";
import { HelpTip } from "../docs/HelpTip";

/** ShedLogs streams shed's own log. */
export function ShedLogs() {
  const { lines, state, clear } = useLogStream("/api/logs");
  return (
    <FillHeight>
      {(height) => (
        <RuntimeLogView
          lines={lines}
          state={state}
          title={
            <>
              <HelpTip topic="shedLog">shed logs</HelpTip>
            </>
          }
          onClear={clear}
          height={height}
        />
      )}
    </FillHeight>
  );
}
