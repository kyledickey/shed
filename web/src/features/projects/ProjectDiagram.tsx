import { GlobeSimpleIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import {
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import type { Deployment, Domain, Service } from "../../api/types";
import { StatusBadge } from "../../components/Badge";
import { deploymentLook } from "../../components/tone";
import { imageName } from "../../lib/names";
import { relativeTime } from "../../lib/time";
import { serviceIcon } from "../services/ServiceHeader";
import { lanes, layers, type Edge } from "./graph";
import { privateAddress } from "./overview";
import styles from "./ProjectDiagram.module.css";

const DOMAINS = "__domains";

type Wire = { key: string; from: string; to: string; d: string };

/** RADIUS rounds the corners of the right-angled connectors. */
const RADIUS = 8;

/** MIN_COLUMN is the narrowest a column gets before the diagram turns to run top to bottom. */
const MIN_COLUMN = 220;

/** LANE is how far the first lane sits left of a stacked node, and the step between lanes. */
const LANE = 14;

const clamp = (lo: number, v: number, hi: number) => Math.min(hi, Math.max(lo, v));

/**
 * fitsSideBySide reports whether n columns fit across width. It mirrors the
 * gap and padding clamps on `.diagram` in the stylesheet.
 */
function fitsSideBySide(n: number, width: number): boolean {
  const gap = clamp(32, width * 0.04, 64);
  const pad = clamp(16, width * 0.03, 48);
  return n * MIN_COLUMN + (n - 1) * gap + 2 * pad <= width;
}

/** connector runs right from (x1, y1), turns at the midpoint, and runs right into (x2, y2). */
function connector(x1: number, y1: number, x2: number, y2: number): string {
  const dy = y2 - y1;
  if (Math.abs(dy) < 1) return `M${x1},${y1} H${x2}`;
  const xm = Math.round((x1 + x2) / 2);
  const r = Math.min(RADIUS, Math.abs(dy) / 2, (x2 - x1) / 2);
  const dir = Math.sign(dy);
  return [
    `M${x1},${y1}`,
    `H${xm - r}`,
    `Q${xm},${y1} ${xm},${y1 + dir * r}`,
    `V${y2 - dir * r}`,
    `Q${xm},${y2} ${xm + r},${y2}`,
    `H${x2}`,
  ].join(" ");
}

/**
 * sideConnector runs left from (x1, y1) to a lane at lx, along the lane,
 * and right into (x2, y2), for nodes stacked in one column.
 */
function sideConnector(x1: number, y1: number, lx: number, x2: number, y2: number): string {
  const dy = y2 - y1;
  const r = Math.min(RADIUS, Math.abs(dy) / 2, x1 - lx);
  const dir = Math.sign(dy) || 1;
  return [
    `M${x1},${y1}`,
    `H${lx + r}`,
    `Q${lx},${y1} ${lx},${y1 + dir * r}`,
    `V${y2 - dir * r}`,
    `Q${lx},${y2} ${lx + r},${y2}`,
    `H${x2}`,
  ].join(" ");
}

/** DiagramService is the part of a service the diagram shows, so sample data can drive it too. */
export type DiagramService = Pick<
  Service,
  | "id"
  | "projectId"
  | "name"
  | "kind"
  | "status"
  | "repo"
  | "branch"
  | "image"
  | "privateHost"
  | "port"
> & {
  domains: Pick<Domain, "id" | "host">[];
  volumes: unknown[];
  latestDeployment: Pick<Deployment, "status" | "createdAt"> | null;
};

/**
 * ProjectDiagram draws a project left to right: its public domains, the
 * services they reach, and the services those use, joined by connectors.
 * Where the columns don't fit side by side, it stacks them top to bottom
 * and runs the connectors in lanes down the left.
 */
export function ProjectDiagram({
  services,
  edges: uses,
  href,
}: {
  services: DiagramService[];
  edges: Edge[];
  /** Where each node links; samples outside the dashboard leave it out. */
  href?: (service: DiagramService) => string;
}) {
  const canvas = useRef<HTMLDivElement>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const refs = useRef(new Map<string, HTMLElement>());
  const [wires, setWires] = useState<Wire[]>([]);
  const [hover, setHover] = useState<string | null>(null);
  const [width, setWidth] = useState<number | null>(null);

  const byId = new Map(services.map((s) => [s.id, s]));
  const exposed = services.filter((s) => s.domains.length > 0);
  const edges: Edge[] = useMemo(
    () => [
      ...services.filter((s) => s.domains.length > 0).map((s) => ({ from: DOMAINS, to: s.id })),
      ...uses,
    ],
    [services, uses],
  );
  const cols = useMemo(
    () =>
      layers(
        services.map((s) => s.id),
        uses,
      ),
    [services, uses],
  );
  const down = width !== null && !fitsSideBySide(cols.length + (exposed.length > 0 ? 1 : 0), width);

  // Stacked, each node's incoming connectors share a lane, and lanes whose
  // stretches overlap sit side by side.
  const lane = useMemo(() => {
    const order = new Map(
      [...(exposed.length > 0 ? [DOMAINS] : []), ...cols.flat()].map((id, i) => [id, i]),
    );
    const spans = new Map<string, { key: string; lo: number; hi: number }>();
    for (const e of edges) {
      const from = (order.get(e.from) ?? 0) + 0.5;
      const to = order.get(e.to) ?? 0;
      const span = spans.get(e.to) ?? { key: e.to, lo: to, hi: to };
      spans.set(e.to, { key: e.to, lo: Math.min(span.lo, from), hi: Math.max(span.hi, from) });
    }
    return lanes([...spans.values()]);
  }, [edges, cols, exposed.length]);
  const laneCount = Math.max(1, ...[...lane.values()].map((l) => l + 1));

  useLayoutEffect(() => {
    const el = canvas.current;
    if (!el) return;
    const ro = new ResizeObserver(([entry]) => setWidth(entry?.contentRect.width ?? null));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  useLayoutEffect(() => {
    const root = wrap.current;
    if (!root) return;
    const measure = () => {
      const box = root.getBoundingClientRect();
      const frame = (id: string) => refs.current.get(id)?.getBoundingClientRect();
      const anchor = (id: string) => {
        const el = refs.current.get(id);
        const head = el?.querySelector<HTMLElement>("[data-anchor]") ?? el;
        return head?.getBoundingClientRect();
      };
      const middle = (r: DOMRect) => Math.round(r.top + r.height / 2 - box.top);
      setWires(
        edges.flatMap((e) => {
          const a = anchor(e.from);
          const b = anchor(e.to);
          const fa = frame(e.from);
          if (!a || !b || !fa) return [];
          const key = `${e.from}>${e.to}`;
          if (down) {
            // Out near the source's foot, in at the target's head.
            const x1 = fa.left - box.left;
            const lx = x1 - LANE * ((lane.get(e.to) ?? 0) + 1);
            const y1 = Math.round(fa.bottom - box.top - 22);
            const x2 = b.left - box.left - 2;
            return [{ key, from: e.from, to: e.to, d: sideConnector(x1, y1, lx, x2, middle(b)) }];
          }
          const x1 = a.right - box.left;
          const x2 = b.left - box.left - 2;
          return [{ key, from: e.from, to: e.to, d: connector(x1, middle(a), x2, middle(b)) }];
        }),
      );
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(root);
    for (const el of refs.current.values()) ro.observe(el);
    document.fonts.addEventListener("loadingdone", measure);
    return () => {
      ro.disconnect();
      document.fonts.removeEventListener("loadingdone", measure);
    };
  }, [edges, down, lane]);

  const register = (id: string) => (el: HTMLElement | null) => {
    if (el) refs.current.set(id, el);
    else refs.current.delete(id);
  };
  const touches = (id: string) =>
    hover === null ||
    hover === id ||
    edges.some((e) => (e.from === hover && e.to === id) || (e.to === hover && e.from === id));
  const hoverProps = (id: string) => ({
    onPointerEnter: () => setHover(id),
    onPointerLeave: () => setHover(null),
    "data-dim": !touches(id) || undefined,
  });

  return (
    <div ref={canvas} className={styles.canvas}>
      <div
        ref={wrap}
        className={styles.diagram}
        data-flow={down ? "down" : "right"}
        style={{ "--gutter": `${LANE * laneCount}px` } as CSSProperties}
      >
        <svg className={styles.wires} aria-hidden>
          <defs>
            <marker
              id="arrow"
              viewBox="0 0 8 8"
              refX="7"
              refY="4"
              markerWidth="8"
              markerHeight="8"
              orient="auto"
            >
              <path d="M1,1 L7,4 L1,7 Z" className={styles.arrowHead} />
            </marker>
            <marker
              id="arrow-lit"
              viewBox="0 0 8 8"
              refX="7"
              refY="4"
              markerWidth="8"
              markerHeight="8"
              orient="auto"
            >
              <path d="M1,1 L7,4 L1,7 Z" className={styles.arrowHeadLit} />
            </marker>
          </defs>
          {wires.map((w) => {
            const lit = hover !== null && (w.from === hover || w.to === hover);
            return (
              <path
                key={w.key}
                d={w.d}
                data-lit={lit || undefined}
                markerEnd={`url(#${lit ? "arrow-lit" : "arrow"})`}
              />
            );
          })}
        </svg>

        {exposed.length > 0 && (
          <div className={styles.column}>
            <Node nodeRef={register(DOMAINS)} {...hoverProps(DOMAINS)}>
              <NodeHead icon={<GlobeSimpleIcon size={16} />} count={countDomains(exposed)}>
                Domains
              </NodeHead>
              <NodeBody>
                {exposed.flatMap((s) =>
                  s.domains.map((d) => (
                    <Row
                      key={d.id}
                      label={<span className={styles.mono}>{d.host}</span>}
                      title={d.host}
                      wideLabel
                    >
                      {s.name}
                    </Row>
                  )),
                )}
              </NodeBody>
            </Node>
          </div>
        )}

        {cols.map((col, i) => (
          <div key={i} className={styles.column}>
            {col.map((id) => {
              const s = byId.get(id)!;
              return (
                <Node key={id} nodeRef={register(id)} {...hoverProps(id)}>
                  <NodeHead
                    icon={serviceIcon(s, 28)}
                    status={<StatusBadge kind="service" status={s.status} size="sm" />}
                  >
                    {href ? (
                      <Link to={href(s)} className={styles.nodeLink}>
                        {s.name}
                      </Link>
                    ) : (
                      s.name
                    )}
                  </NodeHead>
                  <NodeBody>
                    <ServiceRows service={s} />
                  </NodeBody>
                </Node>
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function countDomains(services: DiagramService[]) {
  return services.reduce((n, s) => n + s.domains.length, 0);
}

function ServiceRows({ service: s }: { service: DiagramService }) {
  const d = s.latestDeployment;
  return (
    <>
      {s.kind === "app" && (
        <Row label="Source">
          <span className={styles.mono}>
            {s.repo ? `${s.repo.split("/")[1] ?? s.repo}@${s.branch}` : imageName(s.image ?? "")}
          </span>
        </Row>
      )}
      <Row label="Private">
        <span className={styles.mono}>{privateAddress(s)}</span>
      </Row>
      {s.kind === "app" ? (
        <Row label="Latest">
          {d
            ? `${deploymentLook(d.status).label} · ${relativeTime(d.createdAt)}`
            : "Never deployed"}
        </Row>
      ) : (
        <Row label="Volume">{s.volumes.length > 0 ? "Attached" : "None"}</Row>
      )}
    </>
  );
}

function Node({
  nodeRef,
  children,
  ...rest
}: {
  nodeRef: (el: HTMLElement | null) => void;
  children: ReactNode;
  onPointerEnter: () => void;
  onPointerLeave: () => void;
  "data-dim"?: true;
}) {
  return (
    <div ref={nodeRef} className={styles.frame} {...rest}>
      <div className={styles.panel}>{children}</div>
    </div>
  );
}

function NodeHead({
  icon,
  count,
  status,
  children,
}: {
  icon: ReactNode;
  count?: number;
  status?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className={styles.head} data-anchor>
      <span className={styles.icon}>{icon}</span>
      <span className={styles.name}>{children}</span>
      {count !== undefined && <span className={styles.count}>{count}</span>}
      {status && <span className={styles.status}>{status}</span>}
    </div>
  );
}

function NodeBody({ children }: { children: ReactNode }) {
  return <div className={styles.body}>{children}</div>;
}

/**
 * Row is a label joined to its value by a thin leader line. Short labels
 * keep their width and the value ellipsizes; a wide label shares the room.
 */
function Row({
  label,
  title,
  wideLabel,
  children,
}: {
  label: ReactNode;
  title?: string;
  wideLabel?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={styles.row} title={title} data-wide-label={wideLabel || undefined}>
      <span className={styles.label}>{label}</span>
      <span className={styles.leader} aria-hidden />
      <span className={styles.value}>{children}</span>
    </div>
  );
}
