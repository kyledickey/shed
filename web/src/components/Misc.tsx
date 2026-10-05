import { Box, Check, Copy, Database, Leaf, Zap } from "lucide-react";
import { useState, type CSSProperties, type ReactNode } from "react";
import type { ServiceKind } from "../api/types";
import { cx } from "../lib/cx";
import { Button } from "./Button";
import styles from "./Misc.module.css";
import { kindTones, type Tone } from "./tone";

export function Avatar({ src, name, size = 28 }: { src?: string; name: string; size?: number }) {
  const style = { "--size": `${size}px` } as CSSProperties;
  return src ? (
    <img className={styles.avatar} style={style} src={src} alt={name} />
  ) : (
    <span className={styles.avatar} style={style} data-tone="accent" aria-label={name}>
      {name.slice(0, 1).toUpperCase()}
    </span>
  );
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className={styles.kbd}>{children}</kbd>;
}

const kindIcons: Record<ServiceKind, ReactNode> = {
  app: <Box />,
  postgres: <Database />,
  mysql: <Database />,
  mongo: <Leaf />,
  redis: <Zap />,
};

/** ServiceIcon is a crayon tile for a service kind, or a custom icon. */
export function ServiceIcon({
  kind,
  icon,
  tone,
  size = 32,
}: {
  kind?: ServiceKind;
  icon?: ReactNode;
  tone?: Tone;
  size?: number;
}) {
  return (
    <span
      className={styles.serviceIcon}
      data-tone={tone ?? (kind ? kindTones[kind] : "neutral")}
      style={{ "--size": `${size}px` } as CSSProperties}
    >
      {icon ?? (kind && kindIcons[kind])}
    </span>
  );
}

type CalloutProps = {
  tone?: Tone;
  icon?: ReactNode;
  title?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
};

export function Callout({ tone = "sky", icon, title, children, actions }: CalloutProps) {
  return (
    <div className={styles.callout} data-tone={tone}>
      {icon && <span className={styles.calloutIcon}>{icon}</span>}
      <div className={styles.calloutText}>
        {title && <div className={styles.calloutTitle}>{title}</div>}
        {children && <div className={styles.calloutBody}>{children}</div>}
      </div>
      {actions && <div className={styles.calloutActions}>{actions}</div>}
    </div>
  );
}

export function Skeleton({
  width,
  height = 12,
  radius,
}: {
  width?: number | string;
  height?: number | string;
  radius?: number | string;
}) {
  return <span className={styles.skeleton} style={{ width, height, borderRadius: radius }} />;
}

export function EmptyState({
  icon,
  title,
  description,
  actions,
  tone = "accent",
}: {
  icon: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  tone?: Tone;
}) {
  return (
    <div className={styles.empty}>
      <div className={styles.emptyArt} data-tone={tone}>
        <span className={styles.emptyBlob} />
        <span className={styles.emptyIcon}>{icon}</span>
      </div>
      <h3 className={styles.emptyTitle}>{title}</h3>
      {description && <p className={styles.emptyDescription}>{description}</p>}
      {actions && <div className={styles.emptyActions}>{actions}</div>}
    </div>
  );
}

/** copy writes text to the clipboard. A pending value is handed over as a
 * promise so the write stays within the click's user activation. */
function copy(value: string | (() => Promise<string>)): Promise<void> {
  if (typeof value === "string") return navigator.clipboard.writeText(value);
  const blob = value().then((text) => new Blob([text], { type: "text/plain" }));
  return navigator.clipboard.write([new ClipboardItem({ "text/plain": blob })]);
}

/** CopyButton copies value, or the result of calling it, to the clipboard. */
export function CopyButton({
  value,
  label = "Copy",
}: {
  value: string | (() => Promise<string>);
  label?: string;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      variant="ghost"
      size="sm"
      icon
      aria-label={label}
      className={cx(copied && styles.copied)}
      onClick={() => {
        copy(value).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          },
          () => {},
        );
      }}
    >
      {copied ? <Check size={14} /> : <Copy size={14} />}
    </Button>
  );
}

export function GitHubIcon({ size = 16 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" fill="currentColor" aria-hidden>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  );
}
