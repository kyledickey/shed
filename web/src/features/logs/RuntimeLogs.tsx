import { Eraser } from "lucide-react";
import { useDeferredValue, useState } from "react";
import { useLogStream, type StreamState } from "../../api/events";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { SearchInput } from "../../components/Input";
import { LogViewer } from "../../components/LogViewer";
import styles from "./RuntimeLogs.module.css";

const stateLabels: Record<StreamState, string> = {
  connecting: "Connecting",
  open: "Live",
  reconnecting: "Reconnecting",
  ended: "Stream ended",
  closed: "Disconnected",
};

export function RuntimeLogs({ service }: { service: Service }) {
  const { lines, state, clear } = useLogStream(`/api/services/${service.id}/logs`);
  const [filter, setFilter] = useState("");
  const query = useDeferredValue(filter.trim().toLowerCase());
  const visible = query ? lines.filter((l) => l.text.toLowerCase().includes(query)) : lines;

  const empty =
    service.status === "offline" || service.status === "failed"
      ? "Nothing is running. Deploy the service to see its logs."
      : query && lines.length > 0
        ? `No lines match “${filter}”.`
        : "Waiting for logs…";

  return (
    <div className={styles.wrap}>
      <div className={styles.toolbar}>
        <SearchInput
          className={styles.search}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter logs"
        />
        <span className={styles.state} data-state={state}>
          {stateLabels[state]}
        </span>
        <Button variant="ghost" onClick={clear} disabled={lines.length === 0}>
          <Eraser size={14} />
          Clear
        </Button>
      </div>
      <LogViewer lines={visible} empty={empty} />
    </div>
  );
}
