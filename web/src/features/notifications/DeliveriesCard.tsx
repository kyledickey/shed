import { Inbox } from "lucide-react";
import type { NotificationDelivery } from "../../api/types";
import { Badge } from "../../components/Badge";
import { LayerCard } from "../../components/Card";
import { EmptyState } from "../../components/Misc";
import type { Tone } from "../../components/tone";
import { formatDate, relativeTime } from "../../lib/time";
import styles from "./Notifications.module.css";

const statusLooks: Record<NotificationDelivery["status"], { tone: Tone; label: string }> = {
  pending: { tone: "neutral", label: "Pending" },
  sent: { tone: "grass", label: "Sent" },
  failed: { tone: "tomato", label: "Failed" },
};

/** DeliveriesCard shows the newest notifications and whether they arrived. */
export function DeliveriesCard({ deliveries }: { deliveries: NotificationDelivery[] }) {
  return (
    <LayerCard title="Recent deliveries">
      {deliveries.length === 0 ? (
        <EmptyState
          icon={<Inbox />}
          tone="neutral"
          title="Nothing sent yet"
          description="Notifications appear here once a rule fires. Test notifications are not listed."
        />
      ) : (
        <div className={styles.scroll}>
          <table className={styles.table}>
            <thead>
              <tr>
                <th>Time</th>
                <th>Channel</th>
                <th>Title</th>
                <th>Status</th>
                <th className={styles.num}>Attempts</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((d) => {
                const look = statusLooks[d.status];
                return (
                  <tr key={d.id}>
                    <td className={styles.when} title={formatDate(d.createdAt)}>
                      {relativeTime(d.createdAt)}
                    </td>
                    <td>{d.channelName}</td>
                    <td className={styles.clip}>{d.title}</td>
                    <td>
                      <Badge tone={look.tone} size="sm">
                        {look.label}
                      </Badge>
                    </td>
                    <td className={styles.num}>{d.attempts}</td>
                    <td className={`${styles.clip} ${styles.problem}`}>{d.lastError}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </LayerCard>
  );
}
