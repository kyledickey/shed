import { Globe } from "lucide-react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import type { ServiceKind, ServiceStatus } from "../api/types";
import { StatusBadge } from "./Badge";
import { ServiceIcon } from "./Misc";
import styles from "./ServiceMap.module.css";
import type { Tone } from "./tone";

export type MapNode = {
  id: string;
  name: string;
  kind: ServiceKind;
  status: ServiceStatus;
  icon?: ReactNode;
  tone?: Tone;
  /** Secondary line, e.g. the private host or image. */
  detail?: string;
  /** Public domains; each draws an edge from the internet. */
  domains?: string[];
};

/** MapEdge says `from` depends on `to`, usually through a variable reference. */
export type MapEdge = { from: string; to: string; label?: string };

const INTERNET = "__internet";

/**
 * columns places each node at its longest dependency depth: services nobody
 * depends on sit left, the things they use flow right.
 */
function columns(nodes: MapNode[], edges: MapEdge[]): MapNode[][] {
  const depth = new Map(nodes.map((n) => [n.id, 0]));
  // Relax edges; n passes are enough for a DAG and harmless for cycles.
  for (let pass = 0; pass < nodes.length; pass++) {
    let changed = false;
    for (const e of edges) {
      const d = (depth.get(e.from) ?? 0) + 1;
      if (d > (depth.get(e.to) ?? 0) && d < nodes.length) {
        depth.set(e.to, d);
        changed = true;
      }
    }
    if (!changed) break;
  }
  const cols: MapNode[][] = [];
  for (const n of nodes) (cols[depth.get(n.id) ?? 0] ??= []).push(n);
  const placed = cols.filter(Boolean);

  // Order each column by the average row of its dependents to cut crossings.
  const row = new Map<string, number>();
  placed.forEach((col, c) => {
    if (c > 0) {
      const weight = (n: MapNode) => {
        const rows = edges.filter((e) => e.to === n.id).flatMap((e) => row.get(e.from) ?? []);
        return rows.length ? rows.reduce((a, b) => a + b, 0) / rows.length : Infinity;
      };
      col.sort((a, b) => weight(a) - weight(b));
    }
    col.forEach((n, i) => row.set(n.id, col.length > 1 ? i / (col.length - 1) : 0.5));
  });
  return placed;
}

type Wire = MapEdge & {
  key: string;
  d: string;
  ends: [number, number, number, number];
  at: [number, number];
};

/** Half the column gap in the stylesheet; labels sit in the gap before their target. */
const HALF_GAP = 60;

/**
 * allEdges adds internet edges to public services in the first column.
 * Deeper public services show their domain on the card instead, so no wire
 * has to cross a column to reach them.
 */
function allEdges(cols: MapNode[][], edges: MapEdge[]): MapEdge[] {
  const publicEdges = (cols[0] ?? []).flatMap((n) =>
    (n.domains ?? []).slice(0, 1).map((d) => ({ from: INTERNET, to: n.id, label: d })),
  );
  return [...publicEdges, ...edges];
}

/** curveAt finds the point on a horizontal S-curve at x. */
function curveAt(x1: number, y1: number, x2: number, y2: number, bend: number, x: number) {
  const bx = (t: number) =>
    (1 - t) ** 3 * x1 +
    3 * (1 - t) ** 2 * t * (x1 + bend) +
    3 * (1 - t) * t ** 2 * (x2 - bend) +
    t ** 3 * x2;
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 20; i++) {
    const mid = (lo + hi) / 2;
    if (bx(mid) < x) lo = mid;
    else hi = mid;
  }
  const t = (lo + hi) / 2;
  const y = (1 - t) ** 3 * y1 + 3 * (1 - t) ** 2 * t * y1 + 3 * (1 - t) * t ** 2 * y2 + t ** 3 * y2;
  return [x, y] as [number, number];
}

/** ServiceMap draws a project's services as a left-to-right dependency tree. */
export function ServiceMap({
  nodes,
  edges,
  onSelect,
}: {
  nodes: MapNode[];
  edges: MapEdge[];
  onSelect?: (id: string) => void;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const refs = useRef(new Map<string, HTMLElement>());
  const [wires, setWires] = useState<Wire[]>([]);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [hover, setHover] = useState<string | null>(null);

  const cols = columns(nodes, edges);
  const all = allEdges(cols, edges);
  const firstCol = new Set((cols[0] ?? []).map((n) => n.id));
  const hasPublic = all.some((e) => e.from === INTERNET);

  useLayoutEffect(() => {
    const root = wrap.current;
    if (!root) return;
    const measure = () => {
      const box = root.getBoundingClientRect();
      setSize({ w: root.scrollWidth, h: root.scrollHeight });
      const next: Wire[] = [];
      for (const e of allEdges(columns(nodes, edges), edges)) {
        const a = refs.current.get(e.from)?.getBoundingClientRect();
        const b = refs.current.get(e.to)?.getBoundingClientRect();
        if (!a || !b) continue;
        const x1 = a.right - box.left + root.scrollLeft;
        const y1 = a.top + a.height / 2 - box.top;
        const x2 = b.left - box.left + root.scrollLeft;
        const y2 = b.top + b.height / 2 - box.top;
        const bend = Math.max(40, (x2 - x1) * 0.5);
        next.push({
          ...e,
          key: `${e.from}-${e.to}`,
          d: `M${x1},${y1} C${x1 + bend},${y1} ${x2 - bend},${y2} ${x2},${y2}`,
          ends: [x1, y1, x2, y2],
          at: curveAt(x1, y1, x2, y2, bend, Math.max((x1 + x2) / 2, x2 - HALF_GAP)),
        });
      }
      setWires(next);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(root);
    for (const el of refs.current.values()) ro.observe(el);
    // Web fonts change text widths; re-measure whenever a load finishes.
    document.fonts.addEventListener("loadingdone", measure);
    return () => {
      ro.disconnect();
      document.fonts.removeEventListener("loadingdone", measure);
    };
  }, [nodes, edges]);

  const lit = (w: Wire) => hover !== null && (w.from === hover || w.to === hover);
  const register = (id: string) => (el: HTMLElement | null) => {
    if (el) refs.current.set(id, el);
    else refs.current.delete(id);
  };

  return (
    <div ref={wrap} className={styles.map} data-hovering={hover !== null || undefined}>
      <svg className={styles.wires} width={size.w} height={size.h} aria-hidden>
        {wires.map((w) => (
          <g key={w.key} className={styles.wire} data-lit={lit(w) || undefined}>
            <path d={w.d} />
            <circle cx={w.ends[0]} cy={w.ends[1]} r={3} />
            <circle cx={w.ends[2]} cy={w.ends[3]} r={3} />
          </g>
        ))}
      </svg>
      {wires.map(
        (w) =>
          w.label && (
            <span
              key={w.key}
              className={styles.label}
              data-lit={lit(w) || undefined}
              data-public={w.from === INTERNET || undefined}
              style={{ left: w.at[0], top: w.at[1] }}
            >
              {w.label}
            </span>
          ),
      )}

      {hasPublic && (
        <div className={styles.column}>
          <div
            ref={register(INTERNET)}
            className={styles.internet}
            onPointerEnter={() => setHover(INTERNET)}
            onPointerLeave={() => setHover(null)}
          >
            <Globe size={15} />
            Internet
          </div>
        </div>
      )}
      {cols.map((col, i) => (
        <div key={i} className={styles.column}>
          {col.map((n) => (
            <button
              key={n.id}
              ref={register(n.id)}
              type="button"
              className={styles.node}
              data-dim={
                (hover !== null &&
                  hover !== n.id &&
                  !all.some(
                    (e) =>
                      (e.from === hover && e.to === n.id) || (e.to === hover && e.from === n.id),
                  )) ||
                undefined
              }
              onPointerEnter={() => setHover(n.id)}
              onPointerLeave={() => setHover(null)}
              onClick={() => onSelect?.(n.id)}
            >
              <ServiceIcon kind={n.kind} icon={n.icon} tone={n.tone} size={32} />
              <span className={styles.nodeText}>
                <span className={styles.nodeName}>{n.name}</span>
                {n.detail && <span className={styles.nodeDetail}>{n.detail}</span>}
                {!firstCol.has(n.id) && n.domains?.[0] && (
                  <span className={styles.nodeDomain}>
                    <Globe size={11} />
                    {n.domains[0]}
                  </span>
                )}
              </span>
              {n.status !== "active" && <StatusBadge kind="service" status={n.status} size="sm" />}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}
