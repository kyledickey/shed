import { LayerCard, Table } from "@cloudflare/kumo";
import type { Service } from "../../api/types";
import { HelpTip } from "../docs/HelpTip";
import styles from "./Variables.module.css";

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
    <LayerCard>
      <LayerCard.Secondary className={styles.head}>
        <span className={styles.title}>
          <HelpTip topic="injectedVariables">Provided by shed</HelpTip>
        </span>
        <span className={styles.meta}>Available in every deployment.</span>
      </LayerCard.Secondary>
      <LayerCard.Primary className={styles.body}>
        <Table className={styles.table}>
          <Table.Header variant="compact">
            <Table.Row>
              <Table.Head className={styles.keyCol}>Name</Table.Head>
              <Table.Head>Description</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {vars.map(([key, desc]) => (
              <Table.Row key={key}>
                <Table.Cell className={styles.key}>{key}</Table.Cell>
                <Table.Cell className={styles.desc}>{desc}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
        <p className={styles.reference}>
          Reference these as <code>{"${{ KEY }}"}</code>. Use another service's variables with{" "}
          <code>{"${{ postgres.DATABASE_URL }}"}</code>.
        </p>
      </LayerCard.Primary>
    </LayerCard>
  );
}
