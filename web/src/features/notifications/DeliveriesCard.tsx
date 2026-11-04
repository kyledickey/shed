import { Table } from "@cloudflare/kumo";
import type { NotificationDelivery } from "../../api/types";
import { Spinner } from "../../components/Badge";
import { Well } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { formatDate, relativeTime } from "../../lib/time";
import form from "../settings/Form.module.css";
import { Group } from "../settings/Group";
import styles from "./Notifications.module.css";

const statusLabels: Record<NotificationDelivery["status"], string> = {
  pending: "Pending",
  sent: "Sent",
  failed: "Failed",
};

/** DeliveriesCard shows the newest notifications and whether they arrived. */
export function DeliveriesCard({ deliveries }: { deliveries: NotificationDelivery[] }) {
  return (
    <Group title="Recent deliveries">
      {deliveries.length === 0 ? (
        <Well empty>
          Nothing sent yet. Notifications appear here once a rule fires; test notifications are not
          listed.
        </Well>
      ) : (
        <div className={form.table}>
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Time</Table.Head>
                <Table.Head>Channel</Table.Head>
                <Table.Head>Title</Table.Head>
                <Table.Head>Status</Table.Head>
                <Table.Head className={form.cellNum}>Attempts</Table.Head>
                <Table.Head>Error</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {deliveries.map((d) => (
                <Table.Row key={d.id}>
                  <Table.Cell className={form.cellSubtle} title={formatDate(d.createdAt)}>
                    {relativeTime(d.createdAt)}
                  </Table.Cell>
                  <Table.Cell>{d.channelName}</Table.Cell>
                  <Table.Cell className={styles.clip}>{d.title}</Table.Cell>
                  <Table.Cell>
                    <span className={cx(form.status, d.status === "failed" && form.statusFailed)}>
                      {d.status === "pending" && <Spinner size={9} />}
                      {statusLabels[d.status]}
                    </span>
                  </Table.Cell>
                  <Table.Cell className={form.cellNum}>{d.attempts}</Table.Cell>
                  <Table.Cell className={`${styles.clip} ${styles.problem}`}>
                    {d.lastError}
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </div>
      )}
    </Group>
  );
}
