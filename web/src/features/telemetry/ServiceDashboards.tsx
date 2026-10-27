import { useQuery } from "@tanstack/react-query";
import { BellRing, ExternalLink, RotateCcw } from "lucide-react";
import { errorMessage } from "../../api/client";
import { serviceTelemetryQuery, useResetDashboard } from "../../api/telemetry";
import type { DashboardLink } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Button, LinkButton } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Stack } from "../../components/Layout";
import { Callout } from "../../components/Misc";
import { useToast } from "../../components/Overlay";
import { relativeTime } from "../../lib/time";
import { HelpTip } from "../docs/HelpTip";
import styles from "./Telemetry.module.css";

/** ServiceDashboards lists a service's Grafana dashboards and firing alerts while telemetry is on. */
export function ServiceDashboards({ serviceId }: { serviceId: string }) {
  const { data } = useQuery(serviceTelemetryQuery(serviceId));
  if (!data?.folderUrl) return null;
  return (
    <Stack gap={3}>
      {data.firingAlerts.map((a) => (
        <Callout key={a.name} tone="tomato" icon={<BellRing size={16} />} title={a.name}>
          Firing since {relativeTime(a.since)}.
        </Callout>
      ))}
      <LayerCard
        title={
          <>
            Grafana dashboards <HelpTip topic="dashboards" />
          </>
        }
        actions={
          <LinkButton size="sm" href={data.folderUrl} target="_blank" rel="noreferrer">
            <ExternalLink size={14} />
            Open in Grafana
          </LinkButton>
        }
      >
        <table className={styles.table}>
          <tbody>
            {data.dashboards.map((d) => (
              <DashboardRow key={d.uid} serviceId={serviceId} dashboard={d} />
            ))}
          </tbody>
        </table>
      </LayerCard>
    </Stack>
  );
}

function DashboardRow({
  serviceId,
  dashboard: d,
}: {
  serviceId: string;
  dashboard: DashboardLink;
}) {
  const reset = useResetDashboard(serviceId);
  const toast = useToast();
  const onReset = () =>
    reset.mutate(d.uid, {
      onSuccess: () => toast.add({ title: "Dashboard reset", description: d.title, type: "info" }),
      onError: (err) =>
        toast.add({ title: "Couldn't reset", description: errorMessage(err), type: "error" }),
    });
  return (
    <tr>
      <td>
        <a className={styles.name} href={d.url} target="_blank" rel="noreferrer">
          {d.title}
        </a>
      </td>
      <td>
        {d.customized && (
          <Badge size="sm" variant="outline">
            Customized
          </Badge>
        )}{" "}
        {d.outdated && (
          <Badge size="sm" tone="sky">
            Newer default available
          </Badge>
        )}
      </td>
      <td className={styles.num}>
        {d.customized && (
          <Button size="sm" variant="ghost" loading={reset.isPending} onClick={onReset}>
            <RotateCcw size={14} />
            Reset to default
          </Button>
        )}
      </td>
    </tr>
  );
}
