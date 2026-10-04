import { useQueries, type UseQueryResult } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowUpRight, Box, CircleX, Globe, Lock, Package, Rocket } from "lucide-react";
import { useMemo } from "react";
import { variablesQuery } from "../../api/services";
import { isPending, type Service, type Variables } from "../../api/types";
import { Spinner, StatusBadge } from "../../components/Badge";
import { Card, LayerCard } from "../../components/Card";
import { Grid } from "../../components/Layout";
import { GitHubIcon } from "../../components/Misc";
import { ServiceMap, type MapNode } from "../../components/ServiceMap";
import { deploymentLook } from "../../components/tone";
import { relativeTime } from "../../lib/time";
import { serviceIcon } from "../services/ServiceHeader";
import { serviceEdges } from "./graph";
import styles from "./ProjectCanvas.module.css";

const allData = (results: UseQueryResult<Variables>[]) => results.map((r) => r.data);

/** privateAddress is host:port, or just the host when no port is set. */
function privateAddress(service: Service): string {
  return service.port > 0 ? `${service.privateHost}:${service.port}` : service.privateHost;
}

function mapNode(service: Service): MapNode {
  const node: MapNode = {
    id: service.id,
    name: service.name,
    kind: service.kind,
    status: service.status,
    domains: service.domains.map((d) => d.host),
  };
  if (service.kind !== "app") return { ...node, detail: privateAddress(service) };
  if (service.repo) return { ...node, icon: <GitHubIcon />, detail: service.repo };
  return { ...node, icon: <Box />, tone: "teal", detail: service.image };
}

/** ProjectMap draws the project's services, wired by their variable references. */
export function ProjectMap({ projectId, services }: { projectId: string; services: Service[] }) {
  const navigate = useNavigate();
  const variables = useQueries({
    queries: services.map((s) => variablesQuery(s.id)),
    combine: allData,
  });
  const nodes = useMemo(() => services.map(mapNode), [services]);
  const edges = useMemo(() => serviceEdges(services, variables), [services, variables]);

  return (
    <LayerCard
      title="Service map"
      meta={
        edges.length > 0 && `${edges.length} ${edges.length === 1 ? "connection" : "connections"}`
      }
    >
      <ServiceMap
        nodes={nodes}
        edges={edges}
        onSelect={(serviceId) =>
          void navigate({
            to: "/projects/$projectId/services/$serviceId",
            params: { projectId, serviceId },
          })
        }
      />
    </LayerCard>
  );
}

/** ServiceCards lists compact cards for each service in a project. */
export function ServiceCards({ services }: { services: Service[] }) {
  return (
    <Grid min={260}>
      {services.map((s) => (
        <ServiceCard key={s.id} service={s} />
      ))}
    </Grid>
  );
}

function ServiceCard({ service }: { service: Service }) {
  const domain = service.domains[0];
  return (
    <Card interactive className={styles.card}>
      <div className={styles.head}>
        {serviceIcon(service)}
        <Link
          to="/projects/$projectId/services/$serviceId"
          params={{ projectId: service.projectId, serviceId: service.id }}
          className={styles.name}
        >
          {service.name}
        </Link>
        {service.status !== "active" && (
          <StatusBadge kind="service" status={service.status} size="sm" />
        )}
      </div>
      {domain ? (
        <a className={styles.domain} href={domain.url} target="_blank" rel="noreferrer">
          <Globe size={12} />
          <span className={styles.truncate}>{domain.host}</span>
          <ArrowUpRight size={11} />
        </a>
      ) : (
        <span className={styles.line}>
          <Lock size={12} />
          <span className={styles.truncate}>{privateAddress(service)}</span>
        </span>
      )}
      <div className={styles.foot}>
        <Footer service={service} />
      </div>
    </Card>
  );
}

function Footer({ service }: { service: Service }) {
  if (service.kind !== "app") {
    const n = service.volumes.length;
    return (
      <>
        <Package size={12} />
        {n === 0 ? "No volume" : `${n} ${n === 1 ? "volume" : "volumes"}`}
      </>
    );
  }
  const d = service.latestDeployment;
  if (!d) {
    return (
      <>
        <Rocket size={12} />
        No deployments yet
      </>
    );
  }
  const look = deploymentLook(d.status);
  const when = relativeTime(d.createdAt);
  if (isPending(d.status)) {
    return (
      <>
        <Spinner tone={look.tone} size={10} />
        <span className={styles.truncate}>{look.label}</span>
        <span className={styles.when}>{when}</span>
      </>
    );
  }
  const failed = d.status === "failed" || d.status === "crashed";
  const commit = d.commitMessage.split("\n")[0] ?? "";
  const text = failed
    ? commit
      ? `${look.label}: ${commit}`
      : look.label
    : commit || d.image || look.label;
  return (
    <>
      {failed ? <CircleX size={12} className={styles.failed} /> : <Rocket size={12} />}
      <span className={styles.truncate}>{text}</span>
      <span className={styles.when}>{when}</span>
    </>
  );
}
