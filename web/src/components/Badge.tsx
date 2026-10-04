import type { CSSProperties, ReactNode } from "react";
import type { DeploymentStatus, ServiceStatus } from "../api/types";
import { cx } from "../lib/cx";
import styles from "./Badge.module.css";
import { deploymentLook, serviceLook, type Tone } from "./tone";

type BadgeProps = {
  tone?: Tone;
  variant?: "soft" | "outline" | "solid";
  size?: "sm" | "md";
  mono?: boolean;
  icon?: ReactNode;
  className?: string;
  children: ReactNode;
};

export function Badge({
  tone = "neutral",
  variant = "soft",
  size = "md",
  mono,
  icon,
  className,
  children,
}: BadgeProps) {
  return (
    <span
      data-tone={tone}
      className={cx(styles.badge, styles[variant], styles[size], mono && styles.mono, className)}
    >
      {icon}
      {children}
    </span>
  );
}

/**
 * Spinner marks something that is happening right now. Finished states get
 * no indicator at all, so motion always means "in progress".
 */
export function Spinner({ tone = "neutral", size = 10 }: { tone?: Tone; size?: number }) {
  return (
    <span
      data-tone={tone}
      className={styles.spinner}
      style={{ "--size": `${size}px` } as CSSProperties}
      aria-hidden
    />
  );
}

type StatusBadgeProps =
  | { kind: "deployment"; status: DeploymentStatus; size?: "sm" | "md" }
  | { kind: "service"; status: ServiceStatus; size?: "sm" | "md" };

export function StatusBadge(props: StatusBadgeProps) {
  const look =
    props.kind === "deployment" ? deploymentLook(props.status) : serviceLook(props.status);
  return (
    <Badge tone={look.tone} size={props.size}>
      {look.live && <Spinner tone={look.tone} size={props.size === "sm" ? 8 : 9} />}
      {look.label}
    </Badge>
  );
}

/** Count is a small numeric pill, e.g. beside a tab label. */
export function Count({ children }: { children: ReactNode }) {
  return <span className={styles.count}>{children}</span>;
}
