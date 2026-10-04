import { createLink } from "@tanstack/react-router";
import type { ComponentProps, ReactNode } from "react";
import styles from "./Tabs.module.css";

export function Tabs({ label, children }: { label: string; children: ReactNode }) {
  return (
    <nav className={styles.tabs} aria-label={label}>
      {children}
    </nav>
  );
}

function TabAnchor({ className, ...rest }: ComponentProps<"a">) {
  return <a className={[styles.tab, className].filter(Boolean).join(" ")} {...rest} />;
}

/** A router link styled as a tab; TanStack Router marks the active one with data-status. */
export const TabLink = createLink(TabAnchor);
