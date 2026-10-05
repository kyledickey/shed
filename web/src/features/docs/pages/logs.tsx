import { Link } from "@tanstack/react-router";
import { Diagram, Edge, Node, Note } from "../diagram";
import { CodeBlock, Demo, Doc, DocSection, DocTable, Figure } from "../kit";
import { RedactionLab } from "./logs/RedactionLab";

export function LogsDoc() {
  return (
    <Doc
      slug="logs"
      lede="shed keeps three kinds of logs: a file per deployment build, Docker's own log of each running container, and its own application log. All three reach the dashboard as server-sent events."
    >
      <DocSection page="logs" id="build-logs">
        <p>
          Every deployment writes a build log to{" "}
          <code>&lt;data&gt;/logs/&lt;deploymentID&gt;.log</code> (mode <code>0600</code>). It holds
          shed's own <code>==&gt; </code> step headings and the detail lines under them, the output
          of the build (BuildKit progress for Dockerfile and Railpack builds, or the image pull),
          and, from container start until the health check ends, the new container's own stdout and
          stderr, without Docker's timestamps. A line the container prints that begins with{" "}
          <code>==&gt; </code> gets a leading space, so it can never pass for a step heading.
        </p>
        <Figure caption="Where each kind of log lives and how it gets to your browser.">
          <SourcesDiagram />
        </Figure>
        <ul>
          <li>
            <strong>Size cap.</strong> <code>deployments.log_max_mb</code> (default 10, 0 for
            unlimited) bounds each file. At the cap shed writes up to the limit, then{" "}
            <code>==&gt; Log truncated: size limit reached, further output is discarded</code>, and
            silently drops everything after. That includes the final{" "}
            <code>==&gt; Deployment failed</code> line, so on a truncated log trust the deployment's
            status and error, not the end of the file.
          </li>
          <li>
            <strong>Lifetime.</strong> A log is deleted with its deployment when history is pruned
            past <code>deployments.keep</code> (default 50 per service), or with its service.
          </li>
          <li>
            <strong>Replay.</strong> Opening the log reads the file from the start, then follows it
            every 500 ms while the deployment is in progress. Followers stop reading at activation,
            so everything about the switchover is written before the deployment turns active.
          </li>
        </ul>
        <p>
          For what the lines mean, see{" "}
          <Link to="/docs/$slug" params={{ slug: "deployments" }} hash="build-log">
            reading a build log
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="logs" id="runtime-logs">
        <p>
          Once a container is running, its output belongs to Docker. shed streams the log of a
          service's <em>active</em> container: the last 500 lines, then new ones as they arrive,
          with stdout and stderr merged. Every line starts with the RFC 3339 timestamp Docker adds,
          which the dashboard parses and shows as a time column.
        </p>
        <CodeBlock title="runtime log lines as streamed" lang="log">
          {`2026-10-04T12:00:03.481920113Z {"level":"info","msg":"listening","port":8080}
2026-10-04T12:00:09.220118742Z GET /health 200 2ms
2026-10-04T12:00:11.907356001Z Error: connect ECONNREFUSED 172.18.0.2:5432`}
        </CodeBlock>
        <ul>
          <li>
            <strong>Rotation.</strong> Containers use Docker's <code>json-file</code> driver with 10
            MiB per file and 3 files. A chatty service keeps roughly its last 30 MiB, which is the
            most the 500-line replay can draw on.
          </li>
          <li>
            <strong>One container per deployment.</strong> A new deployment is a new container with
            a fresh log. When the old container is removed after a switchover, its logs go with it.
          </li>
          <li>
            <strong>When it ends.</strong> The stream ends when the container stops. With no active
            container it ends immediately with an <code>end</code> event.
          </li>
          <li>
            <strong>Redaction</strong> applies to this stream and not to Docker's own copy.{" "}
            <code>docker logs</code> shows the raw output.
          </li>
        </ul>
        <RuntimeCommands />
      </DocSection>

      <DocSection page="logs" id="shed-log">
        <p>
          shed's own log is structured text (<code>time=... level=INFO msg=...</code>), written to
          three places: standard error (so it is in <code>journalctl -u shed</code> under systemd),{" "}
          <code>shed.log</code> next to the config file, and an in-memory ring of the last{" "}
          <strong>1000 lines</strong>. The file is rotated by shed: <code>log.max_size_mb</code> 20,{" "}
          <code>log.max_backups</code> 5, <code>log.max_age_days</code> 30 by default, and{" "}
          <code>log.level</code> filters what is written at all. Caddy logs separately to{" "}
          <code>caddy.log</code> in the same directory, which Caddy rotates itself.
        </p>
        <p>
          <code>GET /api/logs</code> replays the ring and then follows. A follower may fall 256
          lines behind; after that lines are dropped for it rather than slowing the logger. The
          stream never ends on its own. The same view is in the dashboard under Server, Logs. For
          how the ring and the fan-out work, see the{" "}
          <Link to="/docs/$slug" params={{ slug: "internals" }} hash="logging">
            logging internals
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="logs" id="sse">
        <p>
          The log endpoints are plain{" "}
          <a
            href="https://html.spec.whatwg.org/multipage/server-sent-events.html"
            target="_blank"
            rel="noreferrer"
          >
            server-sent events
          </a>
          . The response is <code>text/event-stream</code> with <code>Cache-Control: no-cache</code>{" "}
          and <code>X-Accel-Buffering: no</code>, flushed after every event. An idle stream sends a{" "}
          <code>: ping</code> comment every 15 seconds so proxies keep it open. Authentication is
          the <code>shed_session</code> cookie, which a browser's <code>EventSource</code> sends on
          its own.
        </p>
        <Figure caption="A build log stream from open to close. Each arrow is one SSE event.">
          <SseDiagram />
        </Figure>
        <DocTable
          mono
          head={["Event", "Data", "Sent by"]}
          rows={[
            ["log", "one line of text, without its newline", "all three endpoints"],
            ["status", '{"status":"building"}, sent first and on every change', "build log only"],
            [
              "end",
              "empty; the stream is complete and the client should not reconnect",
              "build and runtime logs",
            ],
          ]}
        />
        <CodeBlock title="curl -N --cookie shed_session=... /api/deployments/{id}/logs" lang="sse">
          {`event: status
data: {"status":"building"}

event: log
data: ==> Building acme/web@3f9c2ab

event: log
data: #6 [build 4/4] RUN npm run build

event: log
data: ==> Starting container

event: log
data: Network: shed-prj7k2, private address web:8080 once healthy

event: log
data: ==> Waiting for /health to become healthy

event: status
data: {"status":"deploying"}

: ping

event: log
data: Healthy after 3s

event: status
data: {"status":"active"}

event: end
data: `}
        </CodeBlock>
        <ul>
          <li>
            <strong>Replay on reconnect.</strong> There are no <code>id:</code> fields and no
            support for <code>Last-Event-ID</code>. A reconnect starts over from the beginning of
            the file, ring, or 500-line tail, so a client must clear what it has when the stream
            opens. The dashboard does.
          </li>
          <li>
            <strong>Chunking.</strong> A line longer than 64 KiB without a newline is split into 64
            KiB pieces instead of buffered without bound, for build replay and runtime logs alike.
            The dashboard shows up to 5000 lines per view and strips ANSI escape sequences.
          </li>
        </ul>
      </DocSection>

      <DocSection page="logs" id="redaction">
        <p>
          shed masks secrets with <code>***</code> before they reach a build log or a runtime
          stream. Redaction is by literal value: shed knows what the secret is and replaces that
          exact string.
        </p>
        <ul>
          <li>
            <strong>What counts as a secret.</strong> The resolved value of every non-empty variable
            you <em>stored</em> on the service, and for builds also the clone URL, its token, and
            their base64 form. Injected values like <code>PORT</code> are not secrets unless you
            stored a variable of the same name.
          </li>
          <li>
            <strong>Short values are a trap.</strong> A stored <code>POSTGRES_USER=postgres</code>{" "}
            or <code>POSTGRES_DB=app</code> masks every <code>postgres</code> and <code>app</code>{" "}
            in that service's logs. Check your stored variable values before relying on short ones.
          </li>
          <li>
            <strong>Literal only.</strong> A base64 or URL-encoded copy of a secret is a different
            string and is not masked. Neither are older log lines already on disk: runtime redaction
            uses the variables as they are now.
          </li>
          <li>
            <strong>Where.</strong> Build output, the container's boot output copied into the build
            log, deployment error messages, and the runtime stream. Not Docker's raw log.
          </li>
        </ul>
        <p>
          Output arrives in arbitrary writes, so a secret can be cut in two. Replacing inside each
          write would let it through. shed buffers instead: after each write it emits everything
          except the last <em>longest secret minus one</em> characters, because only those could
          begin a match that is not complete yet. It takes the earliest match, longest secret first,
          and writes what remains when the stream ends. Try it:
        </p>
        <RedactionLab />
      </DocSection>
    </Doc>
  );
}

function SourcesDiagram() {
  const lanes = [
    {
      y: 16,
      cells: [
        [150, "Build pipeline", "steps + BuildKit"],
        [130, "Redact", "service vars"],
        [210, "Log file", "<data>/logs/<id>.log"],
        [230, "Build log stream", "/api/deployments/{id}/logs"],
      ],
    },
    {
      y: 96,
      cells: [
        [150, "Container", "stdout + stderr"],
        [130, "json-file", "10 MiB x 3 files"],
        [210, "Redact", "stored vars"],
        [230, "Runtime log stream", "/api/services/{id}/logs"],
      ],
    },
    {
      y: 176,
      cells: [
        [150, "shed itself", "structured text"],
        [130, "Fan-out", "three sinks"],
        [210, "Ring buffer", "+ stderr, shed.log"],
        [230, "shed log stream", "/api/logs"],
      ],
    },
  ] as const;
  return (
    <Diagram
      width={800}
      height={250}
      label="Sources, storage, and streams of the three kinds of logs"
    >
      {lanes.map((lane) => {
        let x = 0;
        return (
          <g key={lane.y}>
            {lane.cells.map(([w, title, sub], i) => {
              const at = x;
              x += w + 26;
              return (
                <g key={title}>
                  <Node
                    x={at}
                    y={lane.y}
                    w={w}
                    title={title}
                    sub={sub}
                    tone={i === 3 ? "accent" : "neutral"}
                  />
                  {i < 3 && (
                    <Edge
                      points={[
                        [at + w, lane.y + 26],
                        [at + w + 26, lane.y + 26],
                      ]}
                    />
                  )}
                </g>
              );
            })}
          </g>
        );
      })}
      <Note x={0} y={244}>
        Docker's own copy of container output is never redacted, and neither is shed's log.
      </Note>
    </Diagram>
  );
}

function SseDiagram() {
  const rows: [string, boolean, number][] = [
    ["GET /api/deployments/{id}/logs", true, 40],
    ['event: status  {"status":"building"}', false, 90],
    ["event: log  (replay of the file so far)", false, 140],
    ["event: log  (new lines, polled every 500 ms)", false, 190],
    ['event: status  {"status":"active"}', false, 240],
    ["event: end", false, 290],
  ];
  return (
    <Diagram width={800} height={330} label="A build log stream between the browser and the API">
      <Node x={10} y={10} w={160} h={310} title="Browser" sub="EventSource" />
      <Node x={630} y={10} w={160} h={310} title="shed API" sub="SSE endpoint" tone="accent" />
      {rows.map(([label, request, y]) => (
        <Edge
          key={label}
          points={
            request
              ? [
                  [170, y],
                  [630, y],
                ]
              : [
                  [630, y],
                  [170, y],
                ]
          }
          label={label}
          tone={request ? "neutral" : "accent"}
          flow={!request && label.includes("new lines")}
        />
      ))}
    </Diagram>
  );
}

function RuntimeCommands() {
  return (
    <Demo title="Read it with Docker">
      <CodeBlock title="on the server" lang="sh">
        {`# the active container, raw and unredacted
docker logs --timestamps --tail 500 --follow \\
  $(docker ps -q --filter label=shed.service=<serviceID>)

# every container of the project, including a candidate mid-deploy
docker ps --filter label=shed.project=<projectID> --format '{{.Names}}'`}
      </CodeBlock>
    </Demo>
  );
}
