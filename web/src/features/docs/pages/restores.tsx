import { Link } from "@tanstack/react-router";
import { CodeBlock, Doc, DocSection, DocTable, Figure } from "../kit";
import { FlowDiagram, PhaseDiagram } from "./restores/diagrams";

export function RestoresDoc() {
  return (
    <Doc
      slug="restores"
      lede="A restore replaces a service's data with a backup. shed takes a safety backup first, checks the archive before touching anything, and keeps a copy of what it replaces until the new data is verified."
    >
      <DocSection page="restores" id="flow">
        <p>
          You start a restore with <code>POST /api/backups/&#123;id&#125;/restore</code>. It creates
          a <code>restores</code> row (<code>running</code> while it waits in the queue) and answers
          202. The work happens in the background, in the same single-file queue as backups. Only
          successful service backups can be restored. For shed.db, follow{" "}
          <Link to="/docs/$slug" params={{ slug: "restores" }} hash="shed-db">
            the manual steps
          </Link>
          . The request fails with 409 if shed is busy or the service is fenced, and with 400 for
          shed.db, unsuccessful, or vanished backups.
        </p>
        <Figure caption="Every restore runs these stages. Nothing is replaced until stage 4.">
          <FlowDiagram />
        </Figure>
        <ol>
          <li>
            <strong>Pre-restore backup.</strong> shed takes a backup of the current state with the
            trigger <code>pre-restore</code>, using the policy's compression and upload settings. It
            runs inside the restore's job. If it fails, the restore fails, and nothing has changed.
          </li>
          <li>
            <strong>Check the archive.</strong> The archive is opened from the local file, or
            downloaded from S3 into <code>&lt;restoreID&gt;.download.partial</code>, and decoded all
            the way through to prove it decrypts and decompresses. Volume archives are also scanned
            for unsafe entries: absolute names, names leaving the root, entries beneath a symlink of
            the archive, and hard links leaving their volume. Any one rejects the whole archive.
          </li>
          <li>
            <strong>Hold the service.</strong> shed holds it for the rest of the restore.
            Deployments in progress are canceled. Deploy, redeploy, start, stop, restart, delete,
            and volume deletion are rejected, and the API answers 409. Pushes that arrive during a
            hold get 503 from the webhook, so GitHub reports the delivery as failed; redeliver it or
            deploy by hand.
          </li>
          <li>
            <strong>Replace the data.</strong> Volumes and redis take the volume path. postgres,
            mysql, and mongo take the dump path.
          </li>
          <li>
            <strong>Release the hold.</strong> Unless the service is stopped (by you, or by a fence
            a failure left in place), its active container is started again, recreated if it was
            removed, and routes are applied. If that fails after the data was restored, the restore
            is recorded as failed with that error.
          </li>
        </ol>
      </DocSection>

      <DocSection page="restores" id="volume-restores">
        <p>
          The volume path is used for <code>volume</code> backups and for redis dumps. It replaces
          the service's volumes whose mount path appears in the archive (for redis, the{" "}
          <code>/data</code> volume). Archive paths that are not a volume of the service are logged
          and left alone, and so are volumes the archive doesn't contain.
        </p>
        <p>
          One rule drives the design:{" "}
          <strong>a volume's data is never removed before a verified copy of it exists.</strong>
        </p>
        <Figure caption="Dashed red arrows are failure paths. The pre-restore copies outlive every step but the last.">
          <PhaseDiagram />
        </Figure>
        <ol>
          <li>
            <strong>Fence.</strong> In one transaction, shed inserts a <code>restore_fences</code>{" "}
            row (phase <code>retaining</code>) and sets <code>services.stopped</code>, remembering
            its old value. Nothing starts a stopped service, including boot reconcile, so the fence
            holds across restarts.
          </li>
          <li>
            Stop and remove the active container. The deployment stays <code>active</code>.
          </li>
          <li>
            Copy each volume to a fresh <code>shed-vol-&lt;volumeID&gt;-pre-restore</code> volume,
            check the copy, then set the phase to <code>replacing</code>.
          </li>
          <li>
            Empty each volume (remove and recreate it), extract the archive into it at{" "}
            <code>/</code>, and check it.
          </li>
          <li>
            Lift the fence (restore <code>services.stopped</code> and delete the row, in one
            transaction), then remove the pre-restore volumes.
          </li>
        </ol>
        <p>
          Copies and extractions stream a tar through the Docker archive API between helper
          containers labeled <code>shed.backup=&lt;restoreID&gt;</code>.{" "}
          <code>shed-restore-&lt;id&gt;</code> mounts the volumes writable for the extraction, and{" "}
          <code>shed-restore-&lt;id&gt;-src</code> (read-only) and{" "}
          <code>shed-restore-&lt;id&gt;-dst</code> mount the source and target of a copy at the
          service's mount paths.
        </p>
        <h3>What "check" means</h3>
        <p>
          A check reads the target back and compares it with the tar that was written. Every entry
          must be present with the same type, the same symlink target, and, for files, the same size
          and SHA-256. Hard links compare as the file they link to. Extra entries are allowed, since
          Docker may fill an empty volume with what the image has at the mount path.
        </p>
        <h3>When something fails</h3>
        <DocTable
          head={["Failure", "Result"]}
          rows={[
            [
              "Stopping or copying aside fails",
              "Volumes are unchanged. The fence is lifted and the service starts again.",
            ],
            [
              "Emptying, extracting, or the check fails",
              "The pre-restore copies are put back (empty, copy, check) and the fence is lifted. The error says so.",
            ],
            [
              "Putting back fails too",
              "The fence and the pre-restore volumes are kept. The service stays stopped and fenced, and the error names the volumes holding the previous data.",
            ],
            [
              "shed shuts down while replacing",
              "The fence is kept. The next start puts the data back.",
            ],
          ]}
        />
        <h3>Redis</h3>
        <p>
          A redis RDB is written to <code>/data/dump.rdb</code> and, as a hard link, to{" "}
          <code>/data/appendonlydir/appendonly.aof.1.base.rdb</code> with a manifest naming it as
          the base of a fresh multi-part AOF. A server with <code>appendonly yes</code> loads only
          the AOF and would otherwise start empty.
        </p>
      </DocSection>

      <DocSection page="restores" id="dump-restores">
        <p>
          For postgres, mysql, and mongo <code>dump</code> backups, shed streams the decoded dump
          into the database's own client with <code>docker exec</code>. The container must be
          running; otherwise the restore fails with "the service is not running; start it to restore
          a database dump".
        </p>
        <p>
          A dump restore <strong>replaces</strong> databases, it doesn't merge into them. The dumps
          drop and recreate only what they contain, so shed first drops every other database.
          Databases created after the backup do not survive.
        </p>
        <DocTable
          head={["Engine", "Loaded with", "Dropped first", "Kept"]}
          rows={[
            [
              "postgres",
              <code key="p">psql -v ON_ERROR_STOP=1</code>,
              <>
                Every database via <code>DROP DATABASE … WITH (FORCE)</code> (postgres 13 or newer).
                First, new connections are disallowed and other sessions are terminated.
              </>,
              "postgres and the templates. Roles and objects created after the backup.",
            ],
            [
              "mysql",
              <code key="m">mysql -uroot</code>,
              <>
                Every database, in the loading session, with foreign key checks off. The client
                stops at the first error. <code>lock_wait_timeout=300</code> fails a restore blocked
                by a metadata lock after 5 minutes.
              </>,
              "mysql, sys, information_schema, performance_schema.",
            ],
            [
              "mongo",
              <code key="g">mongorestore --archive --drop</code>,
              <>
                Every database, with <code>mongosh</code>, which reads credentials from the
                environment.
              </>,
              "admin, config, local.",
            ],
          ]}
        />
        <p>
          Postgres has two extra rules. The dump's <code>DROP ROLE</code> and{" "}
          <code>CREATE ROLE</code> for the connected user always fail, so shed filters them out.
          Connections are allowed again afterwards, also when the load fails. Without the terminate
          step, <code>DROP DATABASE</code> would fail while an app is connected and the rest of the
          dump would mix into the old data.
        </p>
        <h3>Failure and cancellation</h3>
        <p>
          The service is fenced in phase <code>loading</code> while the dump loads, and the fence is
          lifted when it succeeds. If the load fails or is canceled by a shutdown, the database may
          hold part of the dump. Canceling <code>docker exec</code> only aborts the stream; the
          command keeps running inside the container. So before the hold is released, shed stops and
          removes the service's containers, which ends the load, and leaves the service stopped. The
          fence row is deleted, and the error tells you to restore a backup again, or to start the
          service to keep the data as it is. If stopping fails, the fence stays and the next boot
          stops the container.
        </p>
      </DocSection>

      <DocSection page="restores" id="fences">
        <p>
          A <code>restore_fences</code> row means a restore is changing, or failed while changing,
          the service's data. While it exists, shed refuses everything that could start or replace
          the service, and the row is authoritative: it survives restarts.
        </p>
        <DocTable
          head={["Blocked while fenced", "Result"]}
          rows={[
            ["Deploy and redeploy (manual deploys, rollbacks)", "409 Conflict"],
            ["Start and restart", "409 Conflict"],
            ["Container recreation (boot reconcile, releasing a hold)", "Refused"],
            ["A new restore into the service", "409 Conflict"],
            [
              "A GitHub push for the service",
              "Skipped with a warning in shed's log. The webhook still answers 202 and no deployment is recorded.",
            ],
          ]}
        />
        <p>
          Stopping and deleting the service still work. The service API exposes the fence as{" "}
          <code>restoreFence</code>, with <code>restoreId</code>, <code>phase</code> (
          <code>retaining</code>, <code>replacing</code>, or <code>loading</code>), and{" "}
          <code>createdAt</code>.
        </p>
        <h3>Clearing a fence</h3>
        <p>
          The fence goes away when its restore finishes, when boot recovery resolves it, or when you
          clear it with <code>POST /api/services/&#123;id&#125;/restore-fence/clear</code> (the
          dashboard's confirmed <strong>Keep current data</strong> action). Clearing deletes the row
          and nothing else: the service stays stopped with the data it has now, and pre-restore
          volumes are left for you to inspect or remove by hand. The next restore of the service
          overwrites them. Clearing is refused with 409 while the service is held, so a restore in
          progress can't be cleared, and clearing a service without a fence does nothing.
        </p>
      </DocSection>

      <DocSection page="restores" id="recovery">
        <p>
          On boot, before the deployer reconciles, shed settles whatever a crash or shutdown
          interrupted. Backup rows first: <code>queued</code> and <code>running</code> backups and{" "}
          <code>running</code> restores become <code>failed</code> ("interrupted by restart"), and{" "}
          <code>uploading</code> backups are settled by what survives. Leftover{" "}
          <code>.partial</code> files and <code>shed.backup</code> helper containers are removed.
          Then each fence is resolved by its phase.
        </p>
        <DocTable
          head={["Fence phase", "On the next boot", "Service afterwards"]}
          rows={[
            [
              <code key="r">retaining</code>,
              "Volumes are unchanged, so the fence is lifted and the pre-restore volumes are removed.",
              "Starts as before the restore.",
            ],
            [
              <code key="p">replacing</code>,
              "The pre-restore copies are put back and checked first, then the fence is lifted. If that fails, the error is logged and the fence stays.",
              "Starts on success. Stays stopped and fenced on failure, and the next boot tries again.",
            ],
            [
              <code key="l">loading</code>,
              "The active container is stopped, because the load may still be running in it, and the fence row is deleted.",
              "Stays stopped. Its data may hold part of the dump.",
            ],
          ]}
        />
        <p>
          One more case: a fenced service that is no longer stopped (only a shed that did not
          enforce fences could have started it). Its data may have changed since, so its active
          container is stopped and <code>stopped</code> is set again, but its fence, data, and
          pre-restore volumes are kept and the error is logged. Recovery never drops a fence because
          the service looks started.
        </p>
        <p>
          The same sweep settles <code>uploading</code> backups: with the local file present, the
          backup is <code>succeeded</code> and local, with the interruption as its{" "}
          <code>remote_error</code>. Without it, the backup is remote-only if the object at its
          recorded <code>remote_key</code> can be read, and <code>failed</code> otherwise. A
          succeeded backup therefore always has a local file or a remote object. On shutdown, the
          running job is canceled, and it and the queued ones are marked <code>failed</code>{" "}
          ("interrupted by shutdown").
        </p>
      </DocSection>

      <DocSection page="restores" id="shed-db">
        <p>
          shed.db can't be restored from the dashboard, because shed is running on it. Recovery is
          manual: stop shed, put the archive in place, start shed. This is also the procedure for a
          new server after the old one is lost. You need your age secret key if backups are
          encrypted. The GitHub App credentials come back with shed.db, but if the new server has a
          different <code>server.url</code>, update the App's webhook and callback URLs to match.{" "}
          See the{" "}
          <Link to="/docs/$slug" params={{ slug: "backups" }} hash="encryption">
            encryption
          </Link>{" "}
          section about storing that key.
        </p>
        <CodeBlock lang="sh">
          {`systemctl stop shed

# fetch the newest shed.db archive from S3 (any S3 client works)...
aws s3 ls s3://<bucket>/<prefix>/system/ --endpoint-url <endpoint>
aws s3 cp s3://<bucket>/<prefix>/system/<id>.db.zst.age . --endpoint-url <endpoint>
# ...or from the old server's disk: <data.dir>/backups/system/

# decrypt (drop age if encryption was off) and decompress
age -d -i key.txt <id>.db.zst.age | zstd -d -o shed.db

mv shed.db /var/lib/shed/shed.db
rm -f /var/lib/shed/shed.db-wal /var/lib/shed/shed.db-shm
systemctl start shed`}
        </CodeBlock>
        <p>
          shed.db runs in WAL mode, so <code>shed.db-wal</code> and <code>shed.db-shm</code> sit
          beside it. They belong to the database you are replacing, so delete them as shown.{" "}
          <code>/var/lib/shed</code> is the default <code>data.dir</code>; use yours if you changed
          it.
        </p>
      </DocSection>
    </Doc>
  );
}
