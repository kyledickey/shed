import type { Service } from "../../api/types";
import { LayerCard } from "../../components/Card";
import styles from "./Variables.module.css";
import { HelpTip } from "../docs/HelpTip";

/** ProvidedVars lists the variables shed injects that other variables can reference. */
export function ProvidedVars({ service }: { service: Service }) {
  const vars: [string, string][] = [
    ...(service.kind === "app" && service.port > 0
      ? ([["PORT", "The port the service listens on."]] as [string, string][])
      : []),
    ["SHED_PROJECT_NAME", "Name of the project."],
    ["SHED_SERVICE_NAME", "Name of this service."],
    ["SHED_PRIVATE_DOMAIN", "Hostname other services use to reach this one."],
    ["SHED_PUBLIC_DOMAIN", "First public domain, if any."],
    ["SHED_GIT_COMMIT_SHA", "Commit being deployed."],
    ["SHED_GIT_BRANCH", "Branch being deployed."],
  ];
  return (
    <LayerCard
      title={
        <>
          Provided by shed <HelpTip topic="injectedVariables" />
        </>
      }
      meta="Available in every deployment."
    >
      <div className={styles.provided}>
        {vars.map(([key, desc]) => (
          <div key={key} className={styles.row}>
            <span className={styles.key}>{key}</span>
            <span className={styles.desc}>{desc}</span>
          </div>
        ))}
        <p className={styles.hint}>
          Reference these as <code>{"${{ KEY }}"}</code>. Use another service's variables with{" "}
          <code>{"${{ postgres.DATABASE_URL }}"}</code>.
        </p>
      </div>
    </LayerCard>
  );
}
