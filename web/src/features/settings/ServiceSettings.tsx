import type { Service } from "../../api/types";
import { BuildSection, DeploySection, SourceSection } from "./AppSections";
import { DangerZone } from "./DangerZone";
import { NetworkingSection } from "./NetworkingSection";
import { VolumesSection } from "./VolumesSection";
import styles from "./Settings.module.css";

/** Settings cards for a service; databases only get networking, volumes, and deletion. */
export function ServiceSettings({ service }: { service: Service }) {
  return (
    <div className={styles.page}>
      {service.kind === "app" && (
        <>
          <SourceSection service={service} />
          <BuildSection service={service} />
          <DeploySection service={service} />
        </>
      )}
      <NetworkingSection service={service} />
      <VolumesSection service={service} />
      <DangerZone service={service} />
    </div>
  );
}
