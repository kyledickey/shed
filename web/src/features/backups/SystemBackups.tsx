import { InfoIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { backupSettingsQuery, systemBackupsPageQuery, systemBackupsQuery } from "../../api/backups";
import { useCursorPages } from "../../api/paging";
import { isBackupActive } from "../../api/types";
import { Well } from "../../components/Settings";
import form from "../settings/Form.module.css";
import { BackupList } from "./BackupList";
import { PolicyCard } from "./PolicyCard";

/** SystemBackups manages backups of shed's own database: the policy, the list, and how to restore. */
export function SystemBackups() {
  const pages = useCursorPages();
  // The first page holds the policy and any backup in progress.
  const { data } = useSuspenseQuery(systemBackupsQuery);
  const { data: page = data, isPlaceholderData } = useQuery(systemBackupsPageQuery(pages.before));
  const { data: settings } = useSuspenseQuery(backupSettingsQuery);

  return (
    <>
      <PolicyCard scope={null} policy={data.policy} s3Configured={settings.s3 !== null} />
      <BackupList
        scope={null}
        backups={page.backups}
        next={page.next}
        pages={pages}
        turning={isPlaceholderData}
        backingUp={data.backups.some(isBackupActive)}
      />
      <Well>
        <div className={form.note}>
          <InfoIcon size={16} />
          <div>
            Restoring shed.db is manual:
            <ol>
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
          </div>
        </div>
      </Well>
    </>
  );
}
