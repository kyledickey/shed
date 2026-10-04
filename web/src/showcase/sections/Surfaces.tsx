import {
  ArrowUpRight,
  Box,
  Ellipsis,
  Eye,
  EyeOff,
  Globe,
  Lock,
  Plus,
  Rocket,
  ShieldCheck,
  TriangleAlert,
  Info,
  CircleCheck,
  CircleX,
  Package,
} from "lucide-react";
import { useState } from "react";
import type { ServiceKind, ServiceStatus } from "../../api/types";
import { Badge, Spinner, StatusBadge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Card, LayerCard } from "../../components/Card";
import { Sparkline } from "../../components/charts/Mini";
import {
  Callout,
  CopyButton,
  EmptyState,
  GitHubIcon,
  ServiceIcon,
  Skeleton,
} from "../../components/Misc";
import { Menu, MenuItem, MenuSeparator } from "../../components/Overlay";
import { ServiceMap, type MapEdge, type MapNode } from "../../components/ServiceMap";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

const projects: {
  name: string;
  kinds: ServiceKind[];
  status: ServiceStatus;
  when: string;
  seed: number;
}[] = [
  {
    name: "acme",
    kinds: ["app", "app", "postgres", "redis"],
    status: "active",
    when: "4m ago",
    seed: 1,
  },
  { name: "blog", kinds: ["app", "mysql"], status: "deploying", when: "just now", seed: 2 },
  { name: "scraper", kinds: ["app", "mongo"], status: "crashed", when: "2h ago", seed: 3 },
];

function wave(seed: number) {
  return Array.from(
    { length: 32 },
    (_, i) =>
      20 + Math.sin(i / 3 + seed) * 8 + Math.sin(i * 1.7 + seed * 3) * 4 + (i > 26 ? seed * 3 : 0),
  );
}

const mapNodes: MapNode[] = [
  {
    id: "web",
    name: "web",
    kind: "app",
    icon: <GitHubIcon />,
    status: "active",
    detail: "acme/web",
    domains: ["acme.dev"],
  },
  {
    id: "api",
    name: "api",
    kind: "app",
    icon: <GitHubIcon />,
    status: "deploying",
    detail: "acme/api",
    domains: ["api.acme.dev"],
  },
  {
    id: "worker",
    name: "worker",
    kind: "app",
    icon: <Box />,
    tone: "teal",
    status: "active",
    detail: "ghcr.io/acme/worker",
  },
  {
    id: "postgres",
    name: "postgres",
    kind: "postgres",
    status: "active",
    detail: "postgres.internal:5432",
  },
  { id: "redis", name: "redis", kind: "redis", status: "crashed", detail: "redis.internal:6379" },
];

const mapEdges: MapEdge[] = [
  { from: "web", to: "api", label: "API_URL" },
  { from: "api", to: "postgres", label: "DATABASE_URL" },
  { from: "api", to: "redis", label: "REDIS_URL" },
  { from: "worker", to: "postgres", label: "DATABASE_URL" },
  { from: "worker", to: "redis", label: "REDIS_URL" },
];

const vars: [string, string][] = [
  ["DATABASE_URL", "${{ postgres.DATABASE_URL }}"],
  ["REDIS_URL", "${{ redis.REDIS_URL }}"],
  ["PORT", "3000"],
  ["SESSION_SECRET", "c2hlZC1zZXNzaW9uLXNlY3JldC1kZW1v"],
  ["PUBLIC_URL", "https://${{ SHED_PUBLIC_DOMAIN }}"],
];

/** VarValue renders a value with ${{ }} references as crayon chips. */
function VarValue({ value }: { value: string }) {
  const parts = value.split(/(\$\{\{\s*[^}]+\s*\}\})/g);
  return (
    <>
      {parts.map((p, i) =>
        p.startsWith("${{") ? (
          <span key={i} className={s.ref} data-tone={p.includes(".") ? "sky" : "grape"}>
            {p}
          </span>
        ) : (
          p
        ),
      )}
    </>
  );
}

export function Surfaces() {
  const [revealed, setRevealed] = useState<ReadonlySet<string>>(new Set());
  return (
    <>
      <Section title="Projects" description="The icon stack previews each project's services.">
        <Grid min={260}>
          {projects.map((p) => (
            <Card key={p.name} interactive className={s.projectCard}>
              <div className={s.projectTop}>
                <div className={s.iconStack}>
                  {p.kinds.map((k, i) => (
                    <ServiceIcon key={i} kind={k} size={30} />
                  ))}
                </div>
                <StatusBadge kind="service" status={p.status} size="sm" />
              </div>
              <div>
                <div className={s.projectName}>{p.name}</div>
                <div className={s.projectMeta}>
                  <span>{p.kinds.length} services</span>
                  <span>·</span>
                  <span>deployed {p.when}</span>
                </div>
              </div>
              <Sparkline
                values={wave(p.seed)}
                tone={p.status === "crashed" ? "tomato" : "accent"}
                height={32}
              />
            </Card>
          ))}
          <button type="button" className={s.newProject}>
            <Plus size={20} />
            New project
          </button>
        </Grid>
      </Section>

      <Section
        title="Service map"
        description="Connections come from variable references. Hover a service to trace what it talks to."
      >
        <LayerCard title="acme" meta="5 services · 5 connections">
          <ServiceMap nodes={mapNodes} edges={mapEdges} />
        </LayerCard>
      </Section>

      <Section title="Service nodes" description="Compact service cards for the project canvas.">
        <Grid min={260}>
          <Card interactive className={s.serviceNode}>
            <div className={s.serviceNodeHead}>
              <ServiceIcon kind="app" icon={<GitHubIcon />} />
              <span className={s.serviceNodeName}>api</span>
            </div>
            <a className={s.serviceNodeLink}>
              <Globe size={12} /> api.acme.dev <ArrowUpRight size={11} />
            </a>
            <div className={s.serviceNodeFoot}>
              <Rocket size={12} /> Add rate limiting · 42m ago
            </div>
          </Card>
          <Card interactive className={s.serviceNode}>
            <div className={s.serviceNodeHead}>
              <ServiceIcon kind="postgres" />
              <span className={s.serviceNodeName}>postgres</span>
            </div>
            <span className={s.serviceNodeLink}>
              <Lock size={12} /> postgres.internal:5432
            </span>
            <div className={s.serviceNodeFoot}>
              <Package size={12} /> 1 volume · 2.1 GB
            </div>
          </Card>
          <Card interactive className={s.serviceNode}>
            <div className={s.serviceNodeHead}>
              <ServiceIcon kind="app" icon={<Box />} tone="teal" />
              <span className={s.serviceNodeName}>worker</span>
              <StatusBadge kind="service" status="deploying" size="sm" />
            </div>
            <span className={s.serviceNodeLink}>ghcr.io/acme/worker:latest</span>
            <div className={s.serviceNodeFoot}>
              <Rocket size={12} /> Deploying…
            </div>
          </Card>
          <Card interactive className={s.serviceNode}>
            <div className={s.serviceNodeHead}>
              <ServiceIcon kind="redis" />
              <span className={s.serviceNodeName}>redis</span>
              <StatusBadge kind="service" status="crashed" size="sm" />
            </div>
            <span className={s.serviceNodeLink}>
              <Lock size={12} /> redis.internal:6379
            </span>
            <div className={s.serviceNodeFoot}>
              <CircleX size={12} /> Crashed 3 times in 10m
            </div>
          </Card>
        </Grid>
      </Section>

      <Grid min={420}>
        <LayerCard
          title="Variables"
          meta="References resolve at deploy time."
          actions={
            <Button size="sm">
              <Plus size={13} /> Add
            </Button>
          }
        >
          <div className={s.list}>
            {vars.map(([k, v]) => {
              const secret = k.includes("SECRET");
              const shown = !secret || revealed.has(k);
              return (
                <div key={k} className={s.listRow}>
                  <span className={s.varKey}>{k}</span>
                  <span className={s.varValue}>
                    {shown ? <VarValue value={v} /> : <span className={s.masked}>••••••••</span>}
                  </span>
                  {secret && (
                    <Button
                      variant="ghost"
                      size="sm"
                      icon
                      aria-label={shown ? "Hide" : "Reveal"}
                      onClick={() =>
                        setRevealed((prev) => {
                          const next = new Set(prev);
                          if (next.has(k)) next.delete(k);
                          else next.add(k);
                          return next;
                        })
                      }
                    >
                      {shown ? <EyeOff size={14} /> : <Eye size={14} />}
                    </Button>
                  )}
                  <CopyButton value={v} />
                </div>
              );
            })}
          </div>
        </LayerCard>

        <LayerCard
          title="Domains"
          meta="Certificates are issued automatically."
          actions={
            <Button size="sm">
              <Plus size={13} /> Add
            </Button>
          }
        >
          <div className={s.list}>
            {[
              { host: "api.acme.dev", state: "ok" },
              { host: "api-4f2a.shed.acme.dev", state: "generated" },
              { host: "beta.acme.dev", state: "pending" },
            ].map((d) => (
              <div key={d.host} className={s.listRow}>
                <ServiceIcon
                  icon={<Globe />}
                  tone={d.state === "pending" ? "sunflower" : "sky"}
                  size={28}
                />
                <span className={s.domainHost}>{d.host}</span>
                {d.state === "ok" && (
                  <Badge tone="grass" size="sm" icon={<ShieldCheck size={11} />}>
                    TLS
                  </Badge>
                )}
                {d.state === "generated" && (
                  <Badge tone="neutral" size="sm">
                    generated
                  </Badge>
                )}
                {d.state === "pending" && (
                  <Badge tone="sunflower" size="sm">
                    <Spinner tone="sunflower" size={8} /> Waiting for DNS
                  </Badge>
                )}
                <Menu
                  trigger={
                    <Button variant="ghost" size="sm" icon aria-label="Domain actions">
                      <Ellipsis size={14} />
                    </Button>
                  }
                >
                  <MenuItem icon={<ArrowUpRight size={14} />}>Open</MenuItem>
                  <MenuItem icon={<ShieldCheck size={14} />}>Retry certificate</MenuItem>
                  <MenuSeparator />
                  <MenuItem danger>Remove</MenuItem>
                </Menu>
              </div>
            ))}
          </div>
        </LayerCard>
      </Grid>

      <Section title="Callouts">
        <div className={s.stack}>
          <Callout tone="sky" icon={<Info size={16} />} title="Wait for CI is on">
            Deployments start once GitHub checks pass on <code>main</code>.
          </Callout>
          <Callout
            tone="sunflower"
            icon={<TriangleAlert size={16} />}
            title="Unapplied changes"
            actions={
              <Button size="sm" variant="primary">
                Deploy changes
              </Button>
            }
          >
            You changed 2 variables. Redeploy to apply them.
          </Callout>
          <Callout tone="tomato" icon={<CircleX size={16} />} title="Build failed">
            <code>npm ci</code> exited with code 1. Check the build logs.
          </Callout>
          <Callout tone="grass" icon={<CircleCheck size={16} />} title="You're live" />
        </div>
      </Section>

      <Grid min={340}>
        <Specimen label="Empty state">
          <EmptyState
            icon={<Rocket />}
            title="No deployments yet"
            description="Push to main or deploy manually to get your first build going."
            actions={
              <Button variant="primary">
                <Rocket size={14} /> Deploy now
              </Button>
            }
          />
        </Specimen>
        <Specimen label="Loading skeletons">
          <div className={s.stack}>
            {[0, 1, 2].map((i) => (
              <div key={i} style={{ display: "flex", gap: 12, alignItems: "center" }}>
                <Skeleton width={32} height={32} radius="50%" />
                <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
                  <Skeleton width={`${70 - i * 12}%`} height={12} />
                  <Skeleton width="40%" height={10} />
                </div>
                <Skeleton width={56} height={20} radius={999} />
              </div>
            ))}
          </div>
        </Specimen>
      </Grid>
    </>
  );
}
