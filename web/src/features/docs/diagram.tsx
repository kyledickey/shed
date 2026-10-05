import { useId, type ReactNode } from "react";
import type { Tone } from "../../components/tone";
import { cx } from "../../lib/cx";
import styles from "./diagram.module.css";

/**
 * Diagram is an SVG canvas in viewBox units that scales to its container.
 * Compose it from Zone, Node, Edge, and Note; coordinates are top-left based.
 */
export function Diagram({
  width,
  height,
  label,
  children,
}: {
  width: number;
  height: number;
  /** Accessible description of the whole diagram. */
  label: string;
  children: ReactNode;
}) {
  return (
    <svg
      className={styles.diagram}
      viewBox={`0 0 ${width} ${height}`}
      style={{ maxWidth: width }}
      role="img"
      aria-label={label}
    >
      {children}
    </svg>
  );
}

/** Zone is a labeled dashed region grouping nodes, e.g. a process or network. */
export function Zone({
  x,
  y,
  w,
  h,
  label,
  tone = "neutral",
}: {
  x: number;
  y: number;
  w: number;
  h: number;
  label: string;
  tone?: Tone;
}) {
  return (
    <g data-tone={tone} className={styles.zone}>
      <rect x={x} y={y} width={w} height={h} rx={14} />
      <text x={x + 12} y={y + 20}>
        {label}
      </text>
    </g>
  );
}

/** Node is a box with a title and an optional monospace detail line. */
export function Node({
  x,
  y,
  w,
  h = 52,
  title,
  sub,
  tone = "neutral",
  emphasis,
  dim,
}: {
  x: number;
  y: number;
  w: number;
  h?: number;
  title: string;
  sub?: string;
  tone?: Tone;
  /** Draw with a tinted fill, for the thing the diagram is about. */
  emphasis?: boolean;
  /** Fade out, e.g. a step that hasn't happened yet. */
  dim?: boolean;
}) {
  const cy = y + h / 2;
  return (
    <g data-tone={tone} className={cx(styles.node, emphasis && styles.emphasis, dim && styles.dim)}>
      <rect x={x} y={y} width={w} height={h} rx={10} />
      <text x={x + w / 2} y={sub ? cy - 4 : cy + 4} className={styles.nodeTitle}>
        {title}
      </text>
      {sub && (
        <text x={x + w / 2} y={cy + 13} className={styles.nodeSub}>
          {sub}
        </text>
      )}
    </g>
  );
}

/**
 * Edge is an arrow through points. Corners are rounded; `flow` animates the
 * dashes to show traffic or data moving.
 */
export function Edge({
  points,
  label,
  labelAt,
  tone = "neutral",
  dashed,
  flow,
  both,
}: {
  points: [number, number][];
  label?: string;
  /** Label position; defaults to the middle of the longest segment. */
  labelAt?: [number, number];
  tone?: Tone;
  dashed?: boolean;
  flow?: boolean;
  /** Arrowheads at both ends. */
  both?: boolean;
}) {
  const id = useId().replace(/:/g, "");
  const at = labelAt ?? midpoint(points);
  return (
    <g data-tone={tone} className={cx(styles.edge, dashed && styles.dashed, flow && styles.flow)}>
      <defs>
        <marker
          id={`arrow-${id}`}
          viewBox="0 0 10 10"
          refX="8.5"
          refY="5"
          markerWidth="7"
          markerHeight="7"
          orient="auto-start-reverse"
        >
          <path d="M1,1 L9,5 L1,9 z" className={styles.head} />
        </marker>
      </defs>
      <path
        d={roundedPath(points, 10)}
        markerEnd={`url(#arrow-${id})`}
        markerStart={both ? `url(#arrow-${id})` : undefined}
      />
      {label && (
        <text x={at[0]} y={at[1] - 6} className={styles.edgeLabel}>
          {label}
        </text>
      )}
    </g>
  );
}

/** Note is free text on the canvas, e.g. a step number or an annotation. */
export function Note({
  x,
  y,
  children,
  anchor = "start",
  mono,
}: {
  x: number;
  y: number;
  children: string;
  anchor?: "start" | "middle" | "end";
  mono?: boolean;
}) {
  return (
    <text x={x} y={y} textAnchor={anchor} className={cx(styles.note, mono && styles.mono)}>
      {children}
    </text>
  );
}

function midpoint(points: [number, number][]): [number, number] {
  let best: [number, number] = points[0] ?? [0, 0];
  let len = -1;
  for (let i = 1; i < points.length; i++) {
    const [ax, ay] = points[i - 1]!;
    const [bx, by] = points[i]!;
    const l = Math.hypot(bx - ax, by - ay);
    if (l > len) {
      len = l;
      best = [(ax + bx) / 2, (ay + by) / 2];
    }
  }
  return best;
}

/** roundedPath joins points with straight segments and rounded corners of radius r. */
function roundedPath(points: [number, number][], r: number): string {
  if (points.length < 2) return "";
  const [first, ...rest] = points;
  let d = `M${first![0]},${first![1]}`;
  for (let i = 0; i < rest.length; i++) {
    const [x, y] = rest[i]!;
    const next = rest[i + 1];
    if (!next) {
      d += ` L${x},${y}`;
      break;
    }
    const prev = i === 0 ? first! : rest[i - 1]!;
    const inLen = Math.hypot(x - prev[0], y - prev[1]);
    const outLen = Math.hypot(next[0] - x, next[1] - y);
    const k = Math.min(r, inLen / 2, outLen / 2);
    const p1 = [x - ((x - prev[0]) / inLen) * k, y - ((y - prev[1]) / inLen) * k];
    const p2 = [x + ((next[0] - x) / outLen) * k, y + ((next[1] - y) / outLen) * k];
    d += ` L${p1[0]},${p1[1]} Q${x},${y} ${p2[0]},${p2[1]}`;
  }
  return d;
}
