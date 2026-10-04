import { Container, Database } from "lucide-react";
import type { ServiceKind } from "../api/types";
import { GitHubIcon } from "./GitHubIcon";
import styles from "./ServiceIcon.module.css";

type ServiceIconProps = { kind: ServiceKind; repo?: string; size?: "sm" | "md" };

export function ServiceIcon({ kind, repo, size = "md" }: ServiceIconProps) {
  const px = size === "sm" ? 14 : 16;
  return (
    <span className={styles.icon} data-size={size} aria-hidden>
      {kind !== "app" ? (
        <Database size={px} />
      ) : repo ? (
        <GitHubIcon size={px} />
      ) : (
        <Container size={px} />
      )}
    </span>
  );
}

export const kindLabels: Record<ServiceKind, string> = {
  app: "App",
  postgres: "PostgreSQL",
  mysql: "MySQL",
  mongo: "MongoDB",
  redis: "Redis",
};
