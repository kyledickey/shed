import type { ReactNode } from "react";
import { cx } from "../lib/cx";
import styles from "./Pill.module.css";

/** Pill is a small outlined label, such as a deployment's source or a setting's state. */
export function Pill({ children, mono }: { children: ReactNode; mono?: boolean }) {
  return <span className={cx(styles.pill, mono && styles.mono)}>{children}</span>;
}

/** CountChip is a small grey square holding a number, beside a title. */
export function CountChip({ children }: { children: ReactNode }) {
  return <span className={styles.count}>{children}</span>;
}
