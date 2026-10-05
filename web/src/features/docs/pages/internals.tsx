import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { DocSlug } from "../registry";
import { CodeBlock, Doc, DocSection, DocTable, Figure, Term } from "../kit";
import {
  AdmissionDiagram,
  ArchiveDiagram,
  BackupQueueDiagram,
  CollectorDiagram,
  HttpChainDiagram,
  LogWritersDiagram,
  RestoreDiagram,
} from "./internals/diagrams";
import { ResolverTrace } from "./internals/ResolverTrace";

/** Operator links a section back to the page that explains the behavior it implements. */
function Operator({ slug, hash, children }: { slug: DocSlug; hash: string; children: ReactNode }) {
  return (
    <Link to="/docs/$slug" params={{ slug }} hash={hash}>
      {children}
    </Link>
  );
}

export function InternalsDoc() {
  return (
    <Doc
      slug="internals"
      lede="How the larger pieces of shed work in code: the types, functions, and locks that implement behavior the other pages describe from the outside. Read Codebase first for how the packages fit together."
    >
      <DocSection page="internals" id="http">
        <p>
          <Term>internal/api</Term> uses a plain <Term>http.ServeMux</Term> with method and path
          patterns, built in <Term>Server.Handler</Term>. There is no router library. Every response
          passes through <Term>logRequests</Term> (a debug log line with the status) and{" "}
          <Term>securityHeaders</Term>. Then the mux picks a route.
        </p>
        <Figure caption="Session routes are registered through authed(); the rest are wired individually in Handler.">
          <HttpChainDiagram />
        </Figure>
        <p>
          Session routes are wrapped by <Term>authed</Term> in{" "}
          <Term>auth.Require(protectMutations(baseURL, requireJSON(s.handle(h))))</Term>. Because{" "}
          <Term>auth.Require</Term> is outermost, an unauthenticated write gets 401, not 403. It
          also rechecks <Term>auth.allowed_users</Term> on every request, so removing a login takes
          effect at once for new requests. Details of the protections are on{" "}
          <Operator slug="security" hash="requests">
            Request protections
          </Operator>
          .
        </p>
        <ul>
          <li>
            <strong>Handlers return errors.</strong> A <Term>handlerFunc</Term> returns{" "}
            <Term>error</Term>, and <Term>Server.handle</Term> passes it to <Term>writeError</Term>,
            which sends <Term>{'{"error": "..."}'}</Term>. Create one with{" "}
            <Term>errorf(status, format, ...)</Term>. <Term>store.ErrNotFound</Term> becomes 404,{" "}
            <Term>store.ErrConflict</Term> 409, and the sentinels of <Term>deploy</Term> and{" "}
            <Term>backup</Term> (<Term>ErrFenced</Term>, <Term>ErrServiceBusy</Term>,{" "}
            <Term>ErrBusy</Term>, and so on) map to 409, 400, or 503 in one switch. Anything else is
            logged and answered as <Term>500 internal error</Term>, so internals never leak.
          </li>
          <li>
            <strong>Bodies.</strong> <Term>decodeJSON</Term> reads at most <Term>maxBody</Term> (1
            MiB) with <Term>http.MaxBytesReader</Term>. An empty body leaves the target unchanged.
            <Term>requireJSON</Term> only checks the media type of POST, PUT, and PATCH.
          </li>
          <li>
            <strong>The SPA.</strong> <Term>spa(fsys)</Term> serves the embedded{" "}
            <Term>web/dist</Term>. A path that is not a file gets <Term>index.html</Term>, so client
            routes survive a reload. A missing <Term>assets/*</Term> file or anything with an
            extension is a 404. Files under <Term>assets/</Term> are cached for a year.
          </li>
          <li>
            <strong>Unknown API paths.</strong> <Term>/api/</Term> catches everything unmatched and
            returns a JSON 404, so a typo never falls through to the SPA.
          </li>
        </ul>

        <h3>Server-sent events</h3>
        <p>
          <Term>startSSE</Term> (sse.go) sets <Term>text/event-stream</Term>,{" "}
          <Term>Cache-Control: no-cache</Term>, and <Term>X-Accel-Buffering: no</Term>, flushes, and
          starts a heartbeat goroutine that writes a <Term>: ping</Term> comment every 15 seconds.
          <Term>sseStream.send(event, data)</Term> writes one event, one <Term>data:</Term> line per
          line of text, and flushes with <Term>http.ResponseController</Term>. Writes hold a mutex
          and become no-ops after <Term>close</Term>. Events have no ids, so a reconnect replays
          history from the start. <Term>lineWriter</Term> turns a byte stream into lines and splits
          lines longer than 64 KiB (<Term>maxLogLineBytes</Term>). See{" "}
          <Operator slug="logs" hash="sse">
            Streaming over SSE
          </Operator>
          .
        </p>

        <h3>The webhook handler</h3>
        <p>
          <Term>Server.webhook</Term> (github.go) does its own checks, in this order:
        </p>
        <ol>
          <li>Set a 10 second read deadline. Require a configured GitHub App.</li>
          <li>
            Check the shape of <Term>X-Hub-Signature-256</Term>: <Term>sha256=</Term> plus 64 hex
            characters, else 401. This happens before the body is read.
          </li>
          <li>
            <Term>webhookGuard.acquire</Term>: a token bucket (burst 8, one token per second) and at
            most 4 requests in flight, else 429.
          </li>
          <li>
            Read the body, at most <Term>maxWebhookBody</Term> (1 MiB), else 413. Then{" "}
            <Term>github.VerifySignature</Term> compares the HMAC in constant time, else 401.
          </li>
          <li>
            Ignore anything but <Term>push</Term>, deleted branches, and empty refs with 202. Parse
            with <Term>github.ParsePush</Term>.
          </li>
          <li>
            <Term>store.ServicesForPush</Term> selects app services whose repo matches without case,
            whose branch matches exactly, and that have auto-deploy on.
          </li>
          <li>
            For each service, <Term>deliveryCache.begin</Term> keys on the SHA-256 of the body and
            the service ID. A completed duplicate is skipped, an in-flight one makes the request
            return 503, and the cache keeps 1,024 entries for 24 hours in memory.
          </li>
          <li>
            <Term>Deployer.Deploy(…, store.TriggerPush, commit)</Term>. A fenced service is skipped
            with a warning. Any other error calls <Term>deliveries.finish(key, false)</Term> to
            forget the key and returns 503, so GitHub's retry is safe.
          </li>
        </ol>
        <p>
          The operator view is{" "}
          <Operator slug="github" hash="webhooks">
            Push webhooks
          </Operator>{" "}
          and{" "}
          <Operator slug="security" hash="webhook-limits">
            Webhook limits
          </Operator>
          .
        </p>
      </DocSection>

      <DocSection page="internals" id="deployer">
        <p>
          <Term>deploy.Deployer</Term> owns every container shed runs for services. It is created by{" "}
          <Term>deploy.New(Config)</Term>, which takes the store, a <Term>deploy.Docker</Term>, a{" "}
          <Term>deploy.Builder</Term>, an optional <Term>deploy.Proxy</Term>, and a function that
          returns the GitHub client when there is one.
        </p>
        <h3>Admission and the worker</h3>
        <Figure caption="Everything above the dashed hold line runs under d.mu. controlMu is taken before mu, never while holding it.">
          <AdmissionDiagram />
        </Figure>
        <ul>
          <li>
            <strong>enqueue</strong> checks, under <Term>d.mu</Term>, that the deployer is not
            stopped, that <Term>admission</Term> allows the service (not being deleted, not held),
            and that no restore fence exists. Then it stores the deployment as <Term>queued</Term>{" "}
            and hands it to the service's worker.
          </li>
          <li>
            <strong>One worker per service</strong> is started on demand and exits when nothing is
            pending. Its <Term>pending</Term> slot holds one deployment. A newer one replaces it,
            and the replaced one is recorded as canceled with <Term>errSuperseded</Term>. It also
            calls <Term>w.cancel(errSuperseded)</Term> on the running one. Services do not wait for
            each other. The only shared slot is the build slot in <Term>build.Builder</Term>.
          </li>
          <li>
            <strong>Cancel causes decide the recorded status.</strong> <Term>job.fail</Term> reads{" "}
            <Term>context.Cause</Term>: <Term>errShutdown</Term> records "interrupted by shutdown"
            as failed, other causes record <Term>canceled</Term>, and <Term>errCIFailed</Term>{" "}
            records <Term>skipped</Term>.
          </li>
          <li>
            <strong>Hold</strong> is how a backup or restore gets exclusive control.{" "}
            <Term>Hold</Term> marks <Term>d.held[serviceID]</Term>, halts the worker with{" "}
            <Term>errHeld</Term>, and returns a <Term>*Held</Term> with <Term>Active</Term>,{" "}
            <Term>Running</Term>, <Term>StopAndRemove</Term>, and <Term>Release</Term>. While held,
            deploy, redeploy, start, stop, restart, and delete return <Term>ErrServiceBusy</Term>.
          </li>
          <li>
            <strong>Fences</strong> are <Term>restore_fences</Term> rows owned by{" "}
            <Term>backup</Term>. <Term>checkFence</Term> turns one into <Term>ErrFenced</Term> for
            deploy, start, and restart. The API maps both errors to 409.
          </li>
        </ul>
        <p>
          The user-facing rules are in{" "}
          <Operator slug="deployments" hash="pipeline">
            The pipeline
          </Operator>
          .
        </p>

        <h3>The job</h3>
        <p>
          <Term>Deployer.run</Term> opens <Term>&lt;data&gt;/logs/&lt;deploymentID&gt;.log</Term>{" "}
          and calls <Term>job.execute</Term>, which runs these in order. Any error goes to{" "}
          <Term>job.fail</Term>, which logs the outcome, removes the candidate container, and if the
          previous container was stopped, restarts it.
        </p>
        <CodeBlock title="pipeline.go: job.execute" lang="go">{`
waitForCI        // apps with wait_for_ci; polls every 10s, up to 60 min
environment      // resolve variables (internal/vars)
logSecrets       // stored values to redact from logs
buildImage       // build.Builder, or pull and resolve an image
detectPort       // lowest exposed TCP port, when the service has none
start            // status deploying; takeOver first if exclusive
checkHealth      // probe every 1s, 120s deadline (watchStartup without a port)
switchOver       // promote, routes, ActivateDeployment, removeOthers
`}</CodeBlock>

        <h3>Replacing a container safely</h3>
        <p>
          <Term>exclusive(svc, vols)</Term> is true when the service has volumes or a public port,
          since two containers cannot share either. Then <Term>start</Term> calls{" "}
          <Term>takeOver</Term> before creating the candidate: <Term>clearStrays</Term> removes
          leftover containers labeled for the service, <Term>stopPrevious</Term> stops the active
          one with a 30 second grace period and remembers it in <Term>job.stoppedPrev</Term>, and{" "}
          every container of the service must then be <Term>idle</Term> (created, exited, or dead).
          If that cannot be confirmed, the deployment fails before the candidate exists and the
          active container stays up.
        </p>
        <p>
          On failure, <Term>fail</Term> restarts the previous container only after{" "}
          <Term>clearStrays</Term> confirms nothing else of the service is left. It uses{" "}
          <Term>context.WithoutCancel</Term>, so a canceled deployment still cleans up. The same
          idea guards service start and restores.
        </p>
        <p>
          Other services overlap. The candidate starts with no private alias, passes{" "}
          <Term>checkHealth</Term>, and only then does <Term>promote</Term> call{" "}
          <Term>ReconnectNetwork</Term> with the service name as alias. Docker cannot edit the
          aliases of a connected container, so <Term>recheckHealth</Term> probes again for up to 10
          seconds. <Term>switchOver</Term> then holds <Term>routesMu</Term>, applies routes with the
          candidate's address through <Term>routesFor</Term>, and calls{" "}
          <Term>store.ActivateDeployment</Term>, one transaction that makes the new deployment
          active and retires the old one. Once routing has succeeded, activation continues on{" "}
          <Term>context.WithoutCancel</Term>, since stopping halfway could remove the container that
          now receives traffic. Finally the old container is disconnected from the network, then
          removed by <Term>removeOthers</Term>, and old images are pruned to the newest{" "}
          <Term>keepImages</Term> (5).
        </p>
        <p>
          See{" "}
          <Operator slug="deployments" hash="zero-downtime">
            Zero-downtime switchover
          </Operator>{" "}
          and{" "}
          <Operator slug="resources" hash="storage-safety">
            Replacement storage safety
          </Operator>
          .
        </p>

        <h3>Routes and health probes</h3>
        <p>
          <Term>routesFor</Term> builds the full route list: the dashboard route from{" "}
          <Term>cmd/shed</Term>, then one route per domain to <Term>upstream(serviceID)</Term>, the
          container IP and the port recorded on the active deployment. A stopped service, a service
          with no active deployment, or a missing container has no route. A Docker or database error
          is returned instead, so <Term>proxy.Apply</Term> is not called and Caddy keeps its last
          good config. <Term>Proxy.Apply</Term> compares the JSON it would load with the last one
          and skips an identical reload. <Term>probe</Term> makes one attempt with a 5 second
          timeout: a TCP connect, or a GET that must answer below 400. See{" "}
          <Operator slug="networking" hash="healthchecks">
            Health checks
          </Operator>
          .
        </p>

        <h3>Boot</h3>
        <p>
          <Term>Reconcile</Term> runs after <Term>backup.Recover</Term> and before the API serves.
          It fails every deployment still queued, waiting, building, or deploying with "interrupted
          by restart" and removes its containers. For each service it keeps only the newest active
          deployment, starts its container if it is missing and the service is not stopped (
          <Term>ensureRunning</Term>), removes the others, and applies routes. <Term>Stop</Term>{" "}
          cancels running jobs with <Term>errShutdown</Term> and leaves queued ones to the next{" "}
          <Term>Reconcile</Term>. See{" "}
          <Operator slug="deployments" hash="reconcile">
            Reconcile on boot
          </Operator>
          .
        </p>
      </DocSection>

      <DocSection page="internals" id="resolver">
        <p>
          <Term>vars.Resolve(self, all)</Term> takes the raw variables of every service in a
          project, keyed by service name, and returns the expanded variables of <Term>self</Term>.
          Its <Term>resolver</Term> keeps two structures: a <Term>resolved</Term> memo of finished
          values and a <Term>visiting</Term> set with a <Term>stack</Term> of the variables being
          expanded.
        </p>
        <ul>
          <li>
            Keys of <Term>self</Term> are visited in sorted order, so the first reported cycle is
            deterministic.
          </li>
          <li>
            <Term>value(ref)</Term> checks the memo first, then returns <Term>""</Term> for a
            variable that does not exist, and only then checks the stack. A missing variable can
            never be part of a cycle.
          </li>
          <li>
            A reference is <Term>{"${{ name }}"}</Term>, matched by <Term>refPattern</Term>. A name
            with a dot is split at the first dot into service and key. Go's <Term>\s</Term> is
            narrower than JavaScript's, which the web port in <Term>lib/vars.ts</Term> accounts for.
          </li>
          <li>
            Limits are checked before bytes are appended: 64 levels deep (
            <Term>vars: reference depth exceeds 64 at web.V064</Term>), 64 KiB per value, and 1 MiB
            of resolved values in total.
          </li>
          <li>
            A cycle error lists the stack from the first repeat:{" "}
            <Term>vars: reference cycle: web.A -&gt; web.B -&gt; web.A</Term>.
          </li>
        </ul>
        <ResolverTrace />
        <h3>What deploy feeds it</h3>
        <p>
          <Term>Deployer.environment</Term> builds the scope: for every service of the project,{" "}
          <Term>injected(...)</Term> merged with the service's stored variables, which win. Injected
          are <Term>SHED_PROJECT_NAME</Term>, <Term>SHED_SERVICE_NAME</Term>,{" "}
          <Term>SHED_PRIVATE_DOMAIN</Term>, <Term>PORT</Term> (apps with a port),{" "}
          <Term>SHED_PUBLIC_DOMAIN</Term> (the first domain), <Term>SHED_GIT_BRANCH</Term>, and{" "}
          <Term>SHED_GIT_COMMIT_SHA</Term>, the last only for the service being deployed. A service
          without a port gets one after the build, from the image, and <Term>execute</Term> calls{" "}
          <Term>environment</Term> again. The result becomes the container's environment, sorted by
          key. Database templates in <Term>internal/catalog</Term> generate 24 character
          alphanumeric passwords from <Term>crypto/rand</Term> with rejection sampling, so no
          character is more likely than another.
        </p>
        <p>
          The operator view is{" "}
          <Operator slug="variables" hash="resolution">
            How resolution works
          </Operator>
          . <Term>lib/vars.ts</Term> mirrors this code for the playground, so change them together.
        </p>
      </DocSection>

      <DocSection page="internals" id="builds">
        <p>
          <Term>build.Builder.Build(ctx, id, Request, out)</Term> does the work, and{" "}
          <Term>deploy</Term> reaches it through <Term>deploy.Builder</Term>. In order:
        </p>
        <ol>
          <li>
            <Term>Request.validate</Term>, then a 30 minute deadline (<Term>buildTimeout</Term>)
            that starts before the wait for the build slot.
          </li>
          <li>
            <Term>acquire</Term> takes the one slot, a channel of capacity one shared by all
            services.
          </li>
          <li>
            <Term>setup</Term>, once per Builder: <Term>docker buildx rm --keep-state shed</Term>,
            then <Term>docker buildx create</Term> with memory, swap, and CPU quota options from{" "}
            <Term>build.memory_mb</Term> and <Term>build.cpus</Term>. Limits only apply when the
            builder container is created, so it is replaced on the first build after boot. The build
            cache survives.
          </li>
          <li>
            <Term>checkDisk</Term> before the clone, and <Term>watchDisk</Term> every 3 seconds
            during the build. Below <Term>min_free_mb</Term> a new build is refused with{" "}
            <Term>ErrLowDisk</Term>. Below half of it the running build is canceled with that cause.
          </li>
          <li>
            A fresh workspace at <Term>&lt;data&gt;/builds/&lt;id&gt;</Term>, removed afterward.{" "}
            <Term>clone</Term> fetches the commit at depth 1, with the token in a scoped HTTP header
            rather than the URL.
          </li>
          <li>
            <Term>findDockerfile</Term> picks the path. With a Dockerfile it runs{" "}
            <Term>docker buildx build --builder shed --load</Term>. Without one it runs{" "}
            <Term>railpack prepare</Term> and then the same build with the Railpack frontend.
          </li>
        </ol>
        <p>
          Variables reach the build as BuildKit secrets, not as build args.{" "}
          <Term>writeSecrets</Term> writes each into a file in a private directory outside the build
          context, skipping names that are not valid identifiers, and the build gets{" "}
          <Term>--secret id=KEY,src=...</Term>. <Term>prepareEnv</Term> withholds host-sensitive
          names (<Term>PATH</Term>, <Term>HOME</Term>, <Term>TMPDIR</Term>, and the <Term>LD_</Term>
          , <Term>DYLD_</Term>, <Term>XDG_</Term>, <Term>GIT_</Term>, and <Term>DOCKER_</Term>{" "}
          prefixes) from the <Term>railpack prepare</Term> process.
        </p>
        <p>
          Back in <Term>job.buildImage</Term>, repo apps are built as{" "}
          <Term>shed/&lt;serviceID&gt;:&lt;deploymentID&gt;</Term>. Image apps and databases are
          pulled and recorded by their resolved local image ID, which is what makes a redeploy
          repeatable. <Term>Redeploy</Term> refuses a record that only has a mutable tag. See{" "}
          <Operator slug="builds" hash="builder">
            The shed builder
          </Operator>{" "}
          and{" "}
          <Operator slug="builds" hash="disk-guard">
            Disk space guard
          </Operator>
          . The sizes and timings are mirrored by <Term>lib/builder.ts</Term>.
        </p>
      </DocSection>

      <DocSection page="internals" id="logging">
        <Figure caption="Every log that can contain a secret passes through build.Redactor. shed's own log does not.">
          <LogWritersDiagram />
        </Figure>
        <h3>The redactor</h3>
        <p>
          <Term>build.Redactor</Term> (redact.go) is an <Term>io.Writer</Term> wrapper that masks
          literal secrets with <Term>***</Term>. It sorts secrets longest first and keeps a tail of
          the longest secret minus one byte in its buffer after every write. Only the bytes before
          that tail are emitted, so a secret split across two writes is still caught. It always
          takes the earliest match, and the longest at that position. <Term>Flush</Term> writes the
          remainder, so you must call it when the stream ends. Writes are processed in 32 KiB
          pieces. <Term>Redact(s)</Term> masks a whole string, which <Term>job.fail</Term> uses on
          error messages.
        </p>
        <p>
          Who supplies the secrets differs by stream. In <Term>Builder.Build</Term> they are the
          clone URL, its password or token, the base64 of its credentials, and every value of{" "}
          <Term>Request.Env</Term>. In <Term>job.follow</Term> and <Term>Deployer.RuntimeLogs</Term>{" "}
          they come from <Term>logSecrets</Term>: the resolved values of the service's own stored
          variables, without injected metadata such as ports.
        </p>
        <h3>Build log files</h3>
        <p>
          <Term>Deployer.logWriter</Term> wraps the file in a <Term>cappedWriter</Term> (when{" "}
          <Term>deployments.log_max_mb</Term> is set) and a <Term>syncWriter</Term> so concurrent
          writers never interleave lines. At the cap it writes <Term>==&gt; Log truncated</Term>{" "}
          once and discards the rest without an error. Pipeline headings start with{" "}
          <Term>==&gt; </Term>. <Term>lineWriter.emit</Term> prefixes container lines that start the
          same way with a space, so they cannot pass as headings. <Term>Deployer.FollowLog</Term>{" "}
          tails the file by polling every 500 ms, reading the status before the log so a finished
          deployment is complete. The dashboard parses the headings with{" "}
          <Term>lib/buildLog.ts</Term>.
        </p>
        <h3>shed's own log</h3>
        <p>
          <Term>newLogger</Term> in <Term>cmd/shed</Term> builds one <Term>slog</Term> text handler
          over <Term>io.MultiWriter(os.Stderr, tail, rotated)</Term>. Stderr comes first because a
          MultiWriter stops at the first failing writer. <Term>rotated</Term> is a{" "}
          <Term>lumberjack.Logger</Term> for <Term>shed.log</Term>. <Term>logtail.Tail</Term> is a
          ring of the last 1,000 lines. <Term>Follow</Term> replays them and then streams new lines
          through a 256-line channel. A slow follower drops lines instead of blocking the logger.
          Caddy writes to <Term>caddy.log</Term> with its own rotation.
        </p>
        <p>
          Operator pages:{" "}
          <Operator slug="logs" hash="redaction">
            Secret redaction
          </Operator>
          ,{" "}
          <Operator slug="logs" hash="build-logs">
            Build logs
          </Operator>
          , and{" "}
          <Operator slug="logs" hash="shed-log">
            shed's own log
          </Operator>
          .
        </p>
      </DocSection>

      <DocSection page="internals" id="metrics">
        <Figure caption="Collector.collect runs once per tick. The first reading of a container only seeds c.prev.">
          <CollectorDiagram />
        </Figure>
        <p>
          <Term>metrics.Collector.Run</Term> collects immediately and then every{" "}
          <Term>Config.Interval</Term> (10 seconds by default). <Term>collect</Term> lists running
          containers labeled <Term>shed.service</Term>, takes one <Term>Stats</Term> per container,
          and compares it with <Term>c.prev[containerID]</Term>. <Term>rates</Term> returns false,
          and nothing is stored, if no time passed or a counter went backwards, as when a container
          restarts. Rates are summed per service, so the two containers of a switchover add up. The
          store prunes samples older than <Term>Config.Retention</Term> (7 days) once an hour.
        </p>
        <ul>
          <li>
            CPU is a percentage of one core: <Term>Δcpu / Δsystem × online CPUs × 100</Term>, or{" "}
            <Term>Δcpu / Δwall</Term> when the system counter does not advance. Memory is a gauge,
            usage minus inactive file cache, computed in <Term>internal/docker</Term>. Network and
            disk are bytes per second.
          </li>
          <li>
            <Term>internal/host</Term> reads <Term>/proc/stat</Term>, <Term>/proc/meminfo</Term>,
            physical network and disk devices from sysfs, and <Term>statfs</Term>.{" "}
            <Term>collectHost</Term> stores host samples the same way, through{" "}
            <Term>hostRates</Term>.
          </li>
          <li>
            <Term>Query</Term> asks for <Term>Buckets</Term> + 1 = 181 buckets.{" "}
            <Term>window(now, d)</Term> aligns buckets to multiples of the step since the Unix
            epoch, so they are stable between queries. <Term>settle</Term> keeps the window if the
            in-progress bucket has data and otherwise shifts it back one step, so the chart does not
            end on an empty bucket. A bucket with no samples is <Term>nil</Term>, which the API
            sends as <Term>null</Term>. <Term>lib/buckets.ts</Term> mirrors this.
          </li>
        </ul>
        <p>
          See{" "}
          <Operator slug="metrics" hash="sampling">
            Container sampling
          </Operator>{" "}
          and{" "}
          <Operator slug="metrics" hash="buckets">
            Ranges and buckets
          </Operator>
          .
        </p>
      </DocSection>

      <DocSection page="internals" id="backups">
        <p>
          <Term>backup.Manager</Term> runs backups and restores. Like <Term>deploy</Term>, it
          declares the interfaces it needs (<Term>Store</Term>, <Term>Docker</Term>,{" "}
          <Term>Services</Term>, <Term>Held</Term>, <Term>Remote</Term>) and gets them from{" "}
          <Term>cmd/shed</Term>. <Term>Recover</Term> must run before{" "}
          <Term>Deployer.Reconcile</Term>, and <Term>Run</Term> after.
        </p>
        <h3>Queue</h3>
        <Figure caption="One job runs at a time across the whole process.">
          <BackupQueueDiagram />
        </Figure>
        <p>
          Jobs are in <Term>Manager.queue</Term> under <Term>m.mu</Term>, and a worker goroutine
          takes them one at a time. A service may have only one queued or running job:{" "}
          <Term>enqueueBackup</Term> returns <Term>ErrBusy</Term> if <Term>busy(serviceID)</Term> or
          the service is paused. <Term>Restore</Term> only counts other restores, so it can queue
          behind its own backup, and it returns <Term>ErrFenced</Term> for a fenced service. The{" "}
          <Term>Run</Term> loop wakes every minute for <Term>tick</Term>, which enqueues scheduled
          backups. After each job, <Term>execute</Term> deletes failed backups older than 30 days.
          On shutdown the running job is canceled and queued jobs are marked failed with
          "interrupted by shutdown".
        </p>
        <h3>Writing an archive</h3>
        <Figure caption="writeArchive: compression comes before encryption.">
          <ArchiveDiagram />
        </Figure>
        <p>
          <Term>produce</Term> picks the method. Services without a database engine, and databases
          that are not running, are archived from their volumes. A stopped database is held through{" "}
          <Term>Services.Hold</Term> while its files are read. A running database is dumped through{" "}
          <Term>execScript</Term>, which runs the engine's command with{" "}
          <Term>docker exec … sh -c</Term> so credentials come from the container's environment and
          are never on a command line. Passwords go through <Term>PGPASSWORD</Term>,{" "}
          <Term>MYSQL_PWD</Term>, or <Term>REDISCLI_AUTH</Term>, and Mongo reads a private config
          file. Failures keep the last 4 KiB of stderr (<Term>stderrTail</Term>). Redis waits for
          idle saves, runs <Term>BGSAVE SCHEDULE</Term>, waits for <Term>rdb_saves</Term> to
          advance, and streams <Term>/data/dump.rdb</Term>, so it needs Redis 7 or newer.
        </p>
        <p>
          <Term>writeArchive</Term> writes <Term>&lt;file&gt;.partial</Term> with{" "}
          <Term>O_EXCL</Term> and mode 0600 through <Term>newEncoder</Term>: zstd at the policy's
          level, optionally wrapped in <Term>age</Term> with an X25519 recipient. It syncs, renames,
          and syncs the directory. zstd uses at most 4 goroutines, and <Term>best</Term> adds a 16
          MiB window. Volume archives come from the Docker archive API on a never-started helper
          container (<Term>shed-backup-&lt;id&gt;</Term>, label <Term>shed.backup</Term>) that
          mounts the volumes read-only. Entries are rooted at each mount path, nested volumes are
          archived once from their own volume, and ownership, modes, and xattrs are kept.
        </p>
        <p>
          After <Term>BackupSucceeded</Term> is recorded, an upload failure only sets{" "}
          <Term>RemoteError</Term>. The backup stays successful and local. Retention (
          <Term>prune</Term>) runs after scheduled backups only. The age identity is created by{" "}
          <Term>ensureIdentity</Term> with an insert-if-absent of <Term>backup.age_identity</Term>
          and a read back, so concurrent saves agree on one key. <Term>
            backup_destinations
          </Term>{" "}
          rows are never deleted, and a backup records its <Term>destination_id</Term>.{" "}
          <Term>internal/s3</Term> wraps minio-go with multipart uploads, and its <Term>Check</Term>{" "}
          writes, reads, and deletes <Term>.shed-check-&lt;random&gt;</Term>. Schedules are 5-field
          cron through <Term>robfig/cron</Term> and are evaluated in UTC unless they carry{" "}
          <Term>CRON_TZ=</Term>. Next-run times live in <Term>m.next</Term>, so runs missed while
          shed was down are skipped.
        </p>
        <h3>Restoring</h3>
        <Figure caption="The fence phase is stored with the fence row, so a restart can resolve it.">
          <RestoreDiagram />
        </Figure>
        <p>
          <Term>Manager.restore</Term> first runs a <Term>store.BackupPreRestore</Term> backup
          inline, since it already owns the job slot. It then fetches the archive (from S3 if the
          local file is gone) and scans it before touching the service. The scan rejects absolute
          names, <Term>..</Term> escapes, entries beneath an archive symlink, and hard links that
          leave the volume. It then calls <Term>Services.Hold</Term>.
        </p>
        <ul>
          <li>
            <strong>Volumes.</strong> <Term>replaceVolumes</Term> inserts the{" "}
            <Term>restore_fences</Term> row (phase <Term>retaining</Term>) in one transaction with{" "}
            <Term>services.stopped</Term>, remembering the old value. It stops the service, copies
            each volume to <Term>shed-vol-&lt;id&gt;-pre-restore</Term> and checks the copy, moves
            to <Term>replacing</Term>, empties the volume and extracts the archive, and verifies by
            reading back type, link target, size, and SHA-256. Extra files are allowed. If
            extraction fails, <Term>putBack</Term> restores the copies.
          </li>
          <li>
            <strong>Dumps.</strong> <Term>loadDump</Term> fences with phase <Term>loading</Term> and
            pipes the dump into the running database through <Term>execScript</Term>. Postgres uses{" "}
            <Term>psql -v ON_ERROR_STOP=1</Term> after terminating sessions and{" "}
            <Term>DROP DATABASE … WITH (FORCE)</Term>. MySQL drops its databases in-session with
            foreign key checks off. Mongo runs <Term>mongorestore --archive --drop</Term>. Canceling
            a <Term>docker exec</Term> only aborts the stream, not the process, so on failure the
            service's containers are stopped and removed before the hold is released.
          </li>
          <li>
            <strong>Redis</strong> is restored like a volume: <Term>redisFiles</Term> writes{" "}
            <Term>dump.rdb</Term> plus a hard link{" "}
            <Term>appendonlydir/appendonly.aof.1.base.rdb</Term> with a manifest, since a server
            with AOF on would otherwise start empty.
          </li>
          <li>
            <strong>Recovery.</strong> <Term>Recover</Term> fails queued and running jobs with
            "interrupted by restart", settles uploads by what survived (local file means succeeded
            with a remote error, a readable remote object means remote-only, otherwise failed),
            removes leftover <Term>.partial</Term> files and <Term>shed.backup</Term> helper
            containers, and runs <Term>recoverFence</Term> for each fence. By phase it either puts
            the previous data back (<Term>replacing</Term>), stops the service and leaves it stopped
            (<Term>loading</Term>), or just lifts the fence. A service started while fenced is
            stopped and stays fenced until the user clears it.
          </li>
          <li>
            <strong>Errors.</strong> <Term>backup.ErrFenced</Term> and{" "}
            <Term>deploy.ErrServiceBusy</Term> both map to 409 in <Term>writeError</Term>.
          </li>
        </ul>
        <p>
          Operator pages:{" "}
          <Operator slug="backups" hash="pipeline">
            Archive pipeline
          </Operator>
          ,{" "}
          <Operator slug="restores" hash="flow">
            How a restore runs
          </Operator>
          ,{" "}
          <Operator slug="restores" hash="fences">
            Restore fences
          </Operator>
          , and{" "}
          <Operator slug="restores" hash="recovery">
            Crash recovery
          </Operator>
          .
        </p>
        <DocTable
          mono
          head={["Sentinel", "Returned when"]}
          rows={[
            ["backup.ErrBusy", "The service already has a queued or running job, or is paused."],
            ["backup.ErrFenced", "A restore would start into a fenced service."],
            ["backup.ErrNoVolumes", "The service has no volumes to back up."],
            [
              "backup.ErrInvalid",
              "The request is wrong, such as a service that was never deployed.",
            ],
            ["backup.ErrStopped", "The manager is shutting down."],
          ]}
        />
      </DocSection>
    </Doc>
  );
}
