import { useSuspenseQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { backupSettingsQuery, systemBackupsQuery } from "../../api/backups";
import { Section, Stack } from "../../components/Layout";
import { Callout } from "../../components/Misc";
import { BackupList } from "./BackupList";
import styles from "./Backups.module.css";
import { PolicyCard } from "./PolicyCard";

/** SystemBackups manages backups of shed's own database. */
export function SystemBackups() {
  const { data } = useSuspenseQuery(systemBackupsQuery);
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);

  return (
    <Section
      title="shed database"
      description="shed.db holds projects, services, and settings. Snapshots follow their own schedule."
    >
      <Stack gap={4}>
        <PolicyCard scope={null} policy={data.policy} s3Configured={settings.s3 !== null} />
        <BackupList scope={null} backups={data.backups} />
        <Callout tone="sky" icon={<Info size={16} />} title="Restoring shed.db is manual">
          <ol className={styles.steps}>
            <li>Stop shed.</li>
            <li>
              Fetch the newest system backup, from <code>data/backups/system</code> or{" "}
              <code>&lt;prefix&gt;/system/</code> in S3.
            </li>
            <li>
              If it is encrypted, decrypt it with <code>age -d -i key.txt</code>.
            </li>
            <li>
              Decompress it into the data directory with{" "}
              <code>zstd -d -o /var/lib/shed/shed.db</code>.
            </li>
            <li>Start shed.</li>
          </ol>
        </Callout>
      </Stack>
    </Section>
  );
}
