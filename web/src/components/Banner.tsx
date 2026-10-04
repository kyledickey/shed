import { CircleAlert, Info } from "lucide-react";
import type { ReactNode } from "react";
import { errorMessage } from "../api/client";
import styles from "./Banner.module.css";

type BannerProps = {
  tone?: "info" | "danger";
  action?: ReactNode;
  children: ReactNode;
};

export function Banner({ tone = "info", action, children }: BannerProps) {
  return (
    <div className={styles.banner} data-tone={tone} role={tone === "danger" ? "alert" : "status"}>
      {tone === "danger" ? <CircleAlert size={16} /> : <Info size={16} />}
      <div className={styles.text}>{children}</div>
      {action}
    </div>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <Banner tone="danger">{errorMessage(error)}</Banner>;
}
