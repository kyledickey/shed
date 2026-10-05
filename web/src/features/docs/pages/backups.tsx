import { Link } from "@tanstack/react-router";
import { CodeBlock, Doc, DocSection, DocTable, Figure } from "../kit";
import { PipelineDiagram } from "./backups/PipelineDiagram";
import { ArchiveDemo } from "./backups/ArchiveDemo";
import { RetentionSim } from "./backups/RetentionSim";

export function BackupsDoc() {
  return (
    <Doc
      slug="backups"
      lede="shed backs up every service that has a volume, and its own database. A backup is one file: your data, compressed with zstd, optionally encrypted with age, kept on disk and in S3. Every format is standard, so you can recover without shed."
    >
      <DocSection page="backups" id="methods">
        <p>
          What gets archived depends on the service and on what it is doing when the backup runs.
          Databases that are up are dumped through their own tools. Everything else has its volumes
          archived.
        </p>
        <DocTable
          head={["Service", "Container", "Method", "Produced by", "File"]}
          rows={[
            ["postgres", "running", "dump", "pg_dumpall --clean --if-exists", "<id>.sql.zst"],
            [
              "mysql",
              "running",
              "dump",
              "mysqldump --all-databases --single-transaction --routines --events --triggers --set-gtid-purged=OFF",
              "<id>.sql.zst",
            ],
            ["mongo", "running", "dump", "mongodump --archive", "<id>.archive.zst"],
            ["redis", "running", "dump", "BGSAVE, then the RDB file", "<id>.rdb.zst"],
            ["database", "stopped", "volume", "tar of its volumes", "<id>.tar.zst"],
            ["app", "either", "volume", "tar of its volumes, read live", "<id>.tar.zst"],
            ["shed.db", "n/a", "sqlite", "VACUUM INTO snapshot", "<id>.db.zst"],
          ]}
        />
        <p>
          Encrypted archives get a <code>.age</code> suffix. The method is chosen when the backup is
          queued and chosen again when it runs. If the service changed state in between, the backup
          row records the method that was actually used.
        </p>
        <h3>How dumps authenticate</h3>
        <p>
          Dumps run with <code>docker exec</code> in the active container, through{" "}
          <code>sh -c</code>, so credentials come from the container's own environment:{" "}
          <code>POSTGRES_USER</code>/<code>POSTGRES_PASSWORD</code>,{" "}
          <code>MYSQL_ROOT_PASSWORD</code>, <code>MONGO_INITDB_ROOT_*</code>,{" "}
          <code>REDIS_PASSWORD</code>. Passwords never appear on a command line. They go through{" "}
          <code>PGPASSWORD</code>, <code>MYSQL_PWD</code>, and <code>REDISCLI_AUTH</code>, and the
          mongo tools read theirs from a private <code>--config</code> file. A failed command's
          error ends with the last 4 KiB of its stderr.
        </p>
        <p>
          The redis dump waits until no background save is running, starts{" "}
          <code>BGSAVE SCHEDULE</code>, waits until <code>rdb_saves</code> has advanced, checks{" "}
          <code>rdb_last_bgsave_status:ok</code>, and streams <code>/data/dump.rdb</code>. It needs
          redis 7 or newer.
        </p>
        <h3>How volume archives work</h3>
        <p>
          shed reads volumes through the Docker archive API from a helper container. The helper is
          created but never started, uses the active deployment's image, mounts the volumes
          read-only, and is named <code>shed-backup-&lt;backupID&gt;</code> with the label{" "}
          <code>shed.backup=&lt;backupID&gt;</code>. Tar entries are rooted at each volume's mount
          path relative to <code>/</code> (for example <code>var/lib/data/...</code>), and
          ownership, modes, and extended attributes are kept. A volume nested inside another's mount
          path is archived once, from its own volume.
        </p>
        <p>
          A stopped database is held while its volumes are read, so nothing starts it mid-archive.
          App volumes are archived without a hold, while the app runs. A service with volumes but no
          active deployment cannot be backed up yet:{" "}
          <code>POST /api/services/&#123;id&#125;/backups</code> returns 400, and scheduled runs
          skip it.
        </p>
      </DocSection>

      <DocSection page="backups" id="pipeline">
        <p>
          The archive is streamed: data flows from the source through zstd and, if encryption is on,
          through age, straight into a file. Nothing is buffered whole in memory. Compression
          happens before encryption, which is the only order that compresses.
        </p>
        <Figure caption="Top: what one backup job does. Bottom: the statuses a backup row moves through.">
          <PipelineDiagram />
        </Figure>
        <ol>
          <li>
            The row starts <code>queued</code>. One backup or restore runs at a time across all of
            shed, and the rest wait in order. A service may have one job queued or running, except
            that a restore can be queued behind its backup. Conflicting requests get 409.
          </li>
          <li>
            While <code>running</code>, the archive is written to{" "}
            <code>&lt;data&gt;/backups/&lt;serviceID or system&gt;/&lt;file&gt;.partial</code>,
            synced to disk, and renamed into place. Only then is <code>local</code> set. A crash can
            leave a <code>.partial</code>, never a half-written archive under its real name.
          </li>
          <li>
            If the policy uploads and S3 is configured, the backup becomes <code>uploading</code>.
            It is still the service's running job, so it cannot be deleted and another backup cannot
            start. The intended <code>destination_id</code> and <code>remote_key</code> are recorded
            before the upload starts.
          </li>
          <li>
            After the upload's outcome is recorded, or right away when there is nothing to upload,
            the backup is <code>succeeded</code> and <code>finished_at</code> is set. An upload
            failure still ends <code>succeeded</code>, with <code>remote_error</code> set and the
            remote location cleared.
          </li>
          <li>
            With <code>keepLocal = 0</code>, the local file is deleted only after the upload
            succeeded and the row was committed with its remote location. Without S3, or after a
            failed upload, the file stays, so a backup is never left without a copy. Failing to
            remove the file leaves a valid backup.
          </li>
          <li>After every job, failed backups older than 30 days are deleted.</li>
        </ol>
        <p>
          If shed restarts mid-job, boot recovery settles each row by what survived. See{" "}
          <Link to="/docs/$slug" params={{ slug: "restores" }} hash="recovery">
            crash recovery
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="backups" id="compression">
        <p>
          The policy's <code>compression</code> picks a zstd encoder level. shed uses a streaming
          encoder with up to four threads. Encoding stays under about 100 MiB of memory.
        </p>
        <DocTable
          mono
          head={["Policy value", "Encoder level", "Use it when"]}
          rows={[
            ["fastest", "about zstd level 1", "CPU is scarce and disk is not."],
            ["default", "about zstd level 3", "You want the usual zstd trade-off."],
            ["better", "about zstd level 7", "A middle path."],
            [
              "best",
              "about zstd level 11, 16 MiB window",
              "The default. Backups are written once and kept a long time.",
            ],
          ]}
        />
        <p>
          The 16 MiB window finds more long-range repetition in dumps, and the <code>zstd</code>{" "}
          command line decodes it without extra flags. Dumps compress well. Volume archives of
          already-compressed files (images, videos) will not shrink much at any level.
        </p>
      </DocSection>

      <DocSection page="backups" id="encryption">
        <p>
          Encryption is one global switch, <code>backup.encrypt</code>. The first time you turn it
          on, shed generates an age X25519 identity and stores it in the <code>settings</code> table
          as <code>backup.age_identity</code>. The insert does nothing if a key exists, and shed
          reads the stored one back, so concurrent saves agree on one identity and a stored identity
          is never replaced.
        </p>
        <ul>
          <li>
            While encryption is on, every new archive, local and remote, is encrypted to that
            identity's recipient.
          </li>
          <li>
            Turning it off keeps the identity. Older archives keep their own <code>encrypted</code>{" "}
            flag, and shed decrypts them with the stored key.
          </li>
          <li>
            Dashboard downloads and restores decrypt for you. Files you take from disk or S3 are
            still <code>.age</code>.
          </li>
        </ul>
        <p>
          The recipient (<code>age1...</code>) is public. The identity (
          <code>AGE-SECRET-KEY-1...</code>) is the secret: reveal it in the backup settings and
          store it off the server. Without it, encrypted backups, including those of shed.db, cannot
          be recovered after the server is lost. The dashboard reveals the secret key on request
          through <code>GET /api/backups/settings/key</code>.
        </p>
      </DocSection>

      <DocSection page="backups" id="s3">
        <p>
          Backups can be copied to any S3-compatible storage. A destination has an endpoint URL,
          region, bucket, prefix, path-style flag, access key ID, and secret. The prefix is stored
          without surrounding slashes and omitted from keys when empty.
        </p>
        <CodeBlock title="object keys">
          {`<prefix>/services/<serviceID>/<file>
<prefix>/system/<file>`}
        </CodeBlock>
        <h3>Destinations are never deleted</h3>
        <p>
          Each location (endpoint, region, bucket, prefix, path-style) is a row in{" "}
          <code>backup_destinations</code>. Saving the settings at an existing location keeps its ID
          and takes the new credentials. Any other location gets a new row. Rows are never deleted,
          and each uploaded backup records its <code>destination_id</code>. Downloads, restores,
          deletes, and pruning of an object always use the destination it was uploaded to, so
          changing or removing the current destination never points old backups somewhere else.
        </p>
        <p>
          The API never returns the secret. Saving or testing a destination with an empty secret
          reuses the stored one. A test writes, reads, and deletes{" "}
          <code>&lt;prefix&gt;/.shed-check-&lt;random&gt;</code>. Large objects use multipart
          upload. Deleting a service keeps its S3 objects as an off-site copy; remove them by hand
          if you don't want them. Deleting a single backup removes its local file and its S3 object.
        </p>
      </DocSection>

      <DocSection page="backups" id="schedule">
        <p>
          One loop wakes every minute and checks each service with volumes (using its stored policy,
          or the default) and the shed.db policy. The default is <code>0 3 * * *</code>, compression{" "}
          <code>best</code>, keep 7 local, upload, keep 30 remote. The shed.db policy lives in{" "}
          <code>settings</code> as <code>backup.system</code> (JSON) with the same fields.
        </p>
        <p>
          Schedules are standard 5-field cron (no seconds) or descriptors like <code>@daily</code>
          and <code>@every 30m</code>. They run in UTC unless prefixed with{" "}
          <code>CRON_TZ=&lt;zone&gt;</code>.
        </p>
        <DocTable
          mono
          head={["Schedule", "Runs"]}
          rows={[
            ["0 3 * * *", "Every day at 03:00 UTC (the default)."],
            ["@daily", "Every day at 00:00 UTC."],
            ["0 */6 * * *", "Every six hours, on the hour."],
            ["@every 30m", "Every 30 minutes, counted from when the schedule was set."],
            ["CRON_TZ=Europe/Berlin 0 4 * * 1", "Mondays at 04:00 Berlin time, DST included."],
          ]}
        />
        <p>
          Next-run times live in memory and are computed from boot (or from when you saved the
          policy). <strong>Runs missed while shed was down are skipped, not caught up.</strong> A
          run that finds its target busy is skipped too. Policies are validated on save: a parsable
          schedule, a known compression, no negative counts, <code>keepLocal ≥ 1</code> unless
          uploading, and <code>keepRemote ≥ 1</code> when uploading.
        </p>
      </DocSection>

      <DocSection page="backups" id="retention">
        <p>
          After each scheduled backup, shed prunes that target's backups. Only{" "}
          <code>succeeded</code> backups with the <code>schedule</code> trigger are ever pruned.
          Manual and <code>pre-restore</code> backups stay until you delete them.
        </p>
        <ul>
          <li>
            The newest <code>keepLocal</code> scheduled backups that have a local file keep it.
            Older ones lose the file.
          </li>
          <li>
            With <code>keepLocal = 0</code>, only backups that are in S3 lose their local file, so a
            failed upload never leaves a backup without a copy.
          </li>
          <li>
            While the policy uploads, <code>keepRemote</code> works the same way for S3 objects.
            With upload off, S3 objects are left alone.
          </li>
          <li>A pruned backup with neither a local file nor an S3 object is deleted.</li>
        </ul>
        <RetentionSim />
      </DocSection>

      <DocSection page="backups" id="downloads">
        <p>
          A download is the archive decrypted but still compressed, read from the local file or else
          from S3. It is named{" "}
          <code>&lt;service name or shed&gt;-&lt;YYYYMMDD-HHMMSS&gt;.&lt;ext&gt;.zst</code> from its
          creation time in UTC, for example <code>postgres-20261004-030000.sql.zst</code>. The
          dashboard links to <code>GET /api/backups/&#123;id&#125;/download</code>.
        </p>
        <p>
          Because the formats are standard, you can recover with nothing but <code>age</code>,{" "}
          <code>zstd</code>, and the database's own client. The builder below turns a service kind
          and a few choices into the archive name, S3 key, and recovery command. The block after it
          covers every format.
        </p>
        <ArchiveDemo />
        <CodeBlock lang="sh">
          {`# raw file from <data>/backups or S3 (encrypted: .zst.age)
age -d -i key.txt <id>.sql.zst.age | zstd -d | psql -U postgres -d postgres
age -d -i key.txt <id>.sql.zst.age | zstd -d | mysql -uroot -p
age -d -i key.txt <id>.archive.zst.age | zstd -d | mongorestore --archive --drop
age -d -i key.txt <id>.rdb.zst.age | zstd -d > dump.rdb
age -d -i key.txt <id>.tar.zst.age | zstd -d | tar -x -C restore
age -d -i key.txt <id>.db.zst.age | zstd -d > shed.db`}
        </CodeBlock>
        <p>
          Volume archives extract relative to <code>/</code>, so extract into a scratch directory
          and copy out what you need. To bring back shed.db itself, see{" "}
          <Link to="/docs/$slug" params={{ slug: "restores" }} hash="shed-db">
            Recovering shed.db
          </Link>
          .
        </p>
      </DocSection>
    </Doc>
  );
}
