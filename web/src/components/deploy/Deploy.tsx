import { CheckIcon, XIcon } from "@phosphor-icons/react";
import type { DeploymentStatus } from "../../api/types";
import { Spinner } from "../Badge";
import styles from "./deploy.module.css";

const stages = [
  { key: "queued", label: "Queued" },
  { key: "building", label: "Build" },
  { key: "deploying", label: "Deploy" },
  { key: "active", label: "Live" },
] as const;

function stageIndex(status: DeploymentStatus): number {
  switch (status) {
    case "queued":
    case "waiting":
      return 0;
    case "building":
      return 1;
    case "deploying":
      return 2;
    default:
      return 3;
  }
}

/**
 * DeploySteps is a progress track through queued → build → deploy → live.
 * `failedAt` marks the stage that failed for failed deployments.
 */
export function DeploySteps({
  status,
  failedAt,
}: {
  status: DeploymentStatus;
  failedAt?: "building" | "deploying";
}) {
  const failed = status === "failed" || status === "crashed";
  const at = failed ? (failedAt === "deploying" ? 2 : 1) : stageIndex(status);
  return (
    <ol className={styles.steps}>
      {stages.map((s, i) => {
        const state =
          failed && i === at
            ? "failed"
            : i < at || status === "active"
              ? "done"
              : i === at
                ? "current"
                : "todo";
        return (
          <li
            key={s.key}
            className={styles.stage}
            data-state={state}
            aria-current={state === "current" ? "step" : undefined}
          >
            <span className={styles.bar} />
            <span className={styles.label}>
              {state === "done" && <CheckIcon size={14} weight="bold" />}
              {state === "failed" && <XIcon size={14} weight="bold" />}
              {state === "current" && <Spinner size={11} />}
              {s.label}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
