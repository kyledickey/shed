import { useRef, type ReactNode } from "react";
import { cx } from "../lib/cx";
import { Count } from "./Badge";
import { useThumb } from "./Form";
import styles from "./Tabs.module.css";

export type TabItem<T extends string> = {
  value: T;
  label: ReactNode;
  icon?: ReactNode;
  count?: number;
};

type TabsProps<T extends string> = {
  items: TabItem<T>[];
  value: T;
  onChange: (value: T) => void;
  /** pill: filled sliding pill (shell nav). line: underline (in-page sections). */
  variant?: "pill" | "line";
  label: string;
};

/** Tabs is a horizontal tab strip with an animated indicator. */
export function Tabs<T extends string>({
  items,
  value,
  onChange,
  variant = "pill",
  label,
}: TabsProps<T>) {
  const ref = useRef<HTMLDivElement>(null);
  const thumb = useThumb(ref, value);
  return (
    <div ref={ref} role="tablist" aria-label={label} className={cx(styles.tabs, styles[variant])}>
      <span className={styles.indicator} style={thumb} aria-hidden />
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          role="tab"
          aria-selected={item.value === value}
          data-value={item.value}
          className={styles.tab}
          onClick={() => onChange(item.value)}
        >
          {item.icon}
          {item.label}
          {item.count !== undefined && <Count>{item.count}</Count>}
        </button>
      ))}
    </div>
  );
}
