import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { Field, Input } from "../../../components/Form";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { generatedHost, health, simulateHealth } from "../lib/networking";
import { Demo, Doc, DocSection, DocTable, Figure } from "../kit";
import styles from "./networking.module.css";

export function NetworkingDoc() {
  return (
    <Doc
      slug="networking"
      lede="Services in a project talk to each other over a private Docker network, by name. Caddy sits in front and sends each public hostname to whichever container is active right now."
    >
      <DocSection page="networking" id="private-network">
        <p>
          Every project has one bridge network named <code>shed-&lt;projectID&gt;</code>. shed
          creates it when the project's first container starts and removes it when you delete the
          project. A project's services never share a network with another project's, so a name only
          resolves inside its own project.
        </p>
        <p>
          Each deployment runs as one container named{" "}
          <code>shed-&lt;serviceID&gt;-&lt;deploymentID&gt;</code> with three labels,{" "}
          <code>shed.project</code>, <code>shed.service</code>, and <code>shed.deployment</code>.
          shed finds containers by these labels when it reconciles after a restart, samples metrics,
          and cleans up after a failed deploy. The restart policy is <code>unless-stopped</code>.
        </p>
        <p>
          The <em>active</em> container carries the service name as a network alias. That alias is
          the service's <strong>private host</strong>: Docker's embedded DNS resolves{" "}
          <code>postgres</code> to the container's address, so an app connects to{" "}
          <code>postgres:5432</code>. Use the container port. Nothing is published on the host
          unless you set a <a href="#public-port">public TCP port</a>.
        </p>
        <Figure caption="One project network. Caddy reaches containers by IP; services reach each other by alias. A candidate has no alias until it is healthy.">
          <NetworkDiagram />
        </Figure>
        <p>
          The alias moves last. A new container starts with only its container name resolvable, so{" "}
          <code>web</code> keeps pointing at the old container while the new one boots. Once the
          candidate passes its <a href="#healthchecks">health check</a>, shed disconnects and
          reconnects it with the alias, asking Docker for the address it already had, and checks it
          again. The previous container is disconnected from the network before its graceful stop,
          so private traffic only ever reaches the new one. See{" "}
          <Link to="/docs/$slug" params={{ slug: "deployments" }} hash="zero-downtime">
            zero-downtime switchover
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="networking" id="proxy">
        <p>
          shed embeds Caddy and drives it with a generated JSON config. There is a single HTTPS
          server, <code>shed</code>, listening on <code>proxy.https_port</code> (443 by default).
          Plain HTTP on <code>proxy.http_port</code> serves ACME challenges and redirects to HTTPS.
          Caddy's admin API is disabled, so the only way routes change is through shed.
        </p>
        <Figure caption="Caddy matches the Host header against one route per hostname. Routes are terminal, and the 404 catch-all is always last.">
          <ProxyDiagram />
        </Figure>
        <ul>
          <li>
            <strong>Dashboard route.</strong> The hostname of <code>server.url</code> goes to{" "}
            <code>server.listen</code>. A listen address of <code>0.0.0.0</code> or an empty host is
            dialed as <code>127.0.0.1</code>.
          </li>
          <li>
            <strong>Service routes.</strong> Each domain of a service goes to{" "}
            <code>&lt;container IP&gt;:&lt;port&gt;</code> of its active deployment, on the project
            network. The port is the one recorded on that deployment, so editing a service's port
            applies from its next deployment.
          </li>
          <li>
            <strong>No route</strong> exists for a service that is stopped, has no active
            deployment, has no port, or whose container is gone. If shed cannot find out (a Docker
            or database error), it fails the update and Caddy keeps its last config rather than
            dropping services.
          </li>
          <li>
            <strong>Reloads.</strong> shed recomputes all routes after anything that changes domains
            or the active deployment. Hostnames are lowercased and sorted, and the config is only
            reloaded when the JSON actually changed. Two routes with the same hostname are rejected.
          </li>
          <li>
            <strong>Certificates.</strong> Names a public CA can issue for use ACME, with{" "}
            <code>proxy.acme_email</code> as contact when set. Anything else, like an IP address or{" "}
            <code>localhost</code>, gets a certificate from Caddy's internal CA, which browsers will
            not trust. Caddy never touches the host's trust store.
          </li>
        </ul>
        <p>
          Try it on a sample route table. The matcher ignores case and a port suffix, like
          Caddy&apos;s host matcher.
        </p>
        <RouteMatcher />
      </DocSection>

      <DocSection page="networking" id="domains">
        <p>
          A service can have any number of domains. Point a DNS <code>A</code> or <code>AAAA</code>{" "}
          record at the server, then add the hostname to the service. Caddy asks for the certificate
          as soon as the route loads, so DNS has to resolve first or issuance fails until it does.
        </p>
        <p>
          The first domain you add is exposed to the service as <code>SHED_PUBLIC_DOMAIN</code>.
          shed validates the name before saving it:
        </p>
        <DocTable
          head={["Rule", "Result"]}
          rows={[
            [
              "Lowercased and trimmed; every label is 1 to 63 characters of a-z, 0-9 and hyphens, not starting or ending with a hyphen; at most 253 characters in total",
              "400 otherwise",
            ],
            ["The hostname is already used by any service", "409"],
            [
              "The hostname is the dashboard host, the host of server.url",
              "409, the dashboard hostname is reserved",
            ],
          ]}
        />
        <p>
          The dashboard hostname is reserved because a service route and the dashboard route would
          otherwise compete for the same <code>Host</code>. shed also refuses to load a config with
          duplicate hosts, so the dashboard route can never be replaced by a workload. A service
          with domains but no port is not routed: the build log says{" "}
          <code>No port, so its domains are not routed</code>.
        </p>
      </DocSection>

      <DocSection page="networking" id="generated-domains">
        <p>
          If <code>proxy.base_domain</code> is set, adding a domain without a hostname generates
          one: <code>&lt;service&gt;-&lt;project&gt;.&lt;base_domain&gt;</code>. The project part is
          its name reduced to ASCII letters and digits, with each run of anything else becoming one
          hyphen. The label is cut at 63 characters, and the name is computed once, when you create
          it. Renaming the project later does not change it.
        </p>
        <p>
          Generated hosts need wildcard DNS: <code>*.apps.example.com</code> pointing at the server.
          Each generated host still gets its own certificate. Without <code>base_domain</code> the
          request fails with 400 and you can only add custom domains.
        </p>
        <GeneratedPreview />
      </DocSection>

      <DocSection page="networking" id="public-port">
        <p>
          Set a <strong>public port</strong> on a service and Docker publishes it on the host:{" "}
          <code>0.0.0.0:&lt;publicPort&gt;</code> to the container port, TCP. Traffic goes straight
          to the container and never through Caddy, so there is no TLS termination, no hostname
          routing, and no certificate. It is how you expose a database or a game server. A public
          port needs a container port, and both are 0 to 65535.
        </p>
        <p>
          Docker maintains its own firewall rules for published ports, which can bypass host
          firewalls such as <code>ufw</code>. Treat a public port as open to the internet.
        </p>
        <p>
          Two containers cannot bind the same host port, so a service with a public port (or with
          volumes) deploys <em>stop-first</em>: shed stops the old container before it starts the
          new one, and there is a short outage. See{" "}
          <Link to="/docs/$slug" params={{ slug: "resources" }} hash="storage-safety">
            replacement storage safety
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="networking" id="healthchecks">
        <p>
          A new container only gets traffic after it proves it is up. With a container port, shed
          probes the container's IP on the project network. Without a{" "}
          <strong>health check path</strong> that is a TCP connect. With a path it is an HTTP{" "}
          <code>GET</code> that must answer 2xx or 3xx. Redirects are not followed, and a 4xx or 5xx
          fails the probe. Each probe times out after 5 seconds.
        </p>
        <Figure caption="The health check bookends the alias change. A failure at any step removes the candidate and leaves the previous container serving.">
          <HealthDiagram />
        </Figure>
        <DocTable
          mono
          head={["Setting", "Value"]}
          rows={[
            ["probe interval", `${health.intervalSec}s`],
            ["deadline", `${health.timeoutSec}s from container start`],
            ["progress line", `"Not ready yet: <error>" every ${health.reportSec}s`],
            [
              "no container port",
              `no probe; watched ${health.startupWatchSec}s, fails if it exits`,
            ],
            ["after the alias moves", `probe again, up to ${health.recheckSec}s`],
          ]}
        />
        <p>
          A container that exits (or sits in <code>restarting</code>) fails the deployment
          immediately with its exit code, and its last output is copied into the build log. The
          second probe exists because reconnecting a container to the network could disturb it. If
          it fails, the deployment fails and the previous container keeps the alias and the routes.
        </p>
        <HealthSimulator />
      </DocSection>
    </Doc>
  );
}

function NetworkDiagram() {
  return (
    <Diagram width={800} height={300} label="A project network with containers and Caddy">
      <Zone x={200} y={40} w={590} h={250} label="network shed-<projectID>" tone="sky" />
      <Node x={10} y={140} w={140} title="Caddy" sub="in the shed process" tone="accent" emphasis />
      <Node x={220} y={84} w={210} title="web" sub="alias web · 172.18.0.4" tone="grape" />
      <Node x={560} y={84} w={210} title="postgres" sub="alias postgres · 172.18.0.2" />
      <Node
        x={220}
        y={204}
        w={210}
        title="web, next deployment"
        sub="no alias until healthy"
        tone="grape"
        dim
      />
      <Node x={560} y={204} w={210} title="redis" sub="alias redis · 172.18.0.3" />
      <Edge
        points={[
          [150, 166],
          [180, 166],
          [180, 110],
          [220, 110],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [430, 110],
          [560, 110],
        ]}
        label="postgres:5432"
        dashed
      />
      <Edge
        points={[
          [430, 230],
          [560, 230],
        ]}
        label="redis:6379"
        dashed
      />
      <Note x={10} y={218}>
        Container name
      </Note>
      <Note x={10} y={236} mono>
        shed-&lt;serviceID&gt;-
      </Note>
      <Note x={10} y={252} mono>
        &lt;deploymentID&gt;
      </Note>
    </Diagram>
  );
}

function ProxyDiagram() {
  return (
    <Diagram width={800} height={290} label="How Caddy picks a route from the Host header">
      <Node x={10} y={124} w={150} title="Request" sub="Host header" />
      <Node
        x={210}
        y={112}
        w={160}
        h={76}
        title="Caddy"
        sub="server shed · :443"
        tone="accent"
        emphasis
      />
      <Node
        x={470}
        y={24}
        w={320}
        title="shed.example.com"
        sub="to 127.0.0.1:3000 (server.listen)"
        tone="accent"
      />
      <Node
        x={470}
        y={104}
        w={320}
        title="app.example.com"
        sub="to 172.18.0.4:8080 (active container)"
        tone="grape"
      />
      <Node x={470} y={184} w={320} title="any other host" sub="static_response 404" dim />
      <Edge
        points={[
          [160, 150],
          [210, 150],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 50],
          [470, 50],
        ]}
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 130],
          [470, 130],
        ]}
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 210],
          [470, 210],
        ]}
        dashed
      />
      <Note x={20} y={268}>
        Routes are sorted by host and each one is terminal; the 404 handler is last.
      </Note>
    </Diagram>
  );
}

function HealthDiagram() {
  const xs = [0, 166, 332, 498, 664];
  const steps = [
    ["Start", "no alias yet"],
    ["Probe", "1s tick · 2m max"],
    ["Add alias", "same IP"],
    ["Probe again", "10s max"],
    ["Route + retire", "old one stops"],
  ] as const;
  return (
    <Diagram width={800} height={120} label="Health check steps before traffic switches">
      {steps.map(([title, sub], i) => (
        <Node
          key={title}
          x={xs[i]!}
          y={20}
          w={136}
          title={title}
          sub={sub}
          emphasis={i === 1 || i === 3}
        />
      ))}
      {xs.slice(0, -1).map((x) => (
        <Edge
          key={x}
          points={[
            [x + 136, 46],
            [x + 166, 46],
          ]}
        />
      ))}
      <Note x={0} y={104}>
        Until the last step, the previous container keeps the alias and the routes.
      </Note>
    </Diagram>
  );
}

const SAMPLE_ROUTES = [
  { host: "shed.example.com", upstream: "127.0.0.1:3000", target: "dashboard and API" },
  { host: "app.example.com", upstream: "172.18.0.4:8080", target: "demo / web" },
  { host: "web-demo.apps.example.com", upstream: "172.18.0.4:8080", target: "demo / web" },
  { host: "api.example.com", upstream: "172.19.0.2:3000", target: "shop / api" },
] as const;

/** matchHost normalizes a Host header like Caddy's host matcher: no port, no trailing dot, case-insensitive. */
function matchHost(header: string): string {
  return header.trim().toLowerCase().replace(/:\d+$/, "").replace(/\.$/, "");
}

function RouteMatcher() {
  const [header, setHeader] = useState("");
  const host = matchHost(header);
  const hit = SAMPLE_ROUTES.find((r) => r.host === host);
  return (
    <Demo title="Try a Host header">
      <DocTable
        mono
        head={["Host", "Goes to", "Upstream"]}
        rows={SAMPLE_ROUTES.map((r) => [r.host, r.target, r.upstream])}
      />
      <div className={styles.toolbar}>
        <div className={styles.grow}>
          <Field
            label="Host header"
            hint="Sample routes above. Case and a port suffix are ignored."
          >
            {(id) => (
              <Input
                id={id}
                mono
                placeholder="app.example.com"
                value={header}
                onChange={(e) => setHeader(e.target.value)}
              />
            )}
          </Field>
        </div>
      </div>
      {host && (
        <p className={styles.result}>
          {hit
            ? `${hit.host} -> ${hit.upstream} (${hit.target})`
            : `no route for ${host}: static_response 404`}
        </p>
      )}
    </Demo>
  );
}

function GeneratedPreview() {
  const [base, setBase] = useState("apps.example.com");
  const [service, setService] = useState("web");
  const [project, setProject] = useState("My Shop!");
  const baseDomain = base.trim().toLowerCase();
  return (
    <Demo title="Generated domain preview">
      <div className={styles.toolbar}>
        <div className={styles.grow}>
          <Field label="Service name">
            {(id) => (
              <Input id={id} mono value={service} onChange={(e) => setService(e.target.value)} />
            )}
          </Field>
        </div>
        <div className={styles.grow}>
          <Field label="Project name">
            {(id) => (
              <Input id={id} mono value={project} onChange={(e) => setProject(e.target.value)} />
            )}
          </Field>
        </div>
        <div className={styles.grow}>
          <Field label="proxy.base_domain">
            {(id) => <Input id={id} mono value={base} onChange={(e) => setBase(e.target.value)} />}
          </Field>
        </div>
      </div>
      <p className={styles.result}>
        {baseDomain
          ? generatedHost(service, project, baseDomain)
          : "Without a base domain shed can't generate a host."}
      </p>
    </Demo>
  );
}

function HealthSimulator() {
  const [port, setPort] = useState(8080);
  const [path, setPath] = useState("/health");
  const [readyAfter, setReadyAfter] = useState(7);
  const run = simulateHealth({ name: "web", port, path: path.trim(), readyAfter });
  return (
    <Demo title="Replay a health check">
      <div className={styles.toolbar}>
        <div className={styles.grow}>
          <Field label="Container port" hint="0 means no port, so shed only watches the container.">
            {(id) => (
              <Input
                id={id}
                mono
                inputMode="numeric"
                value={String(port)}
                onChange={(e) => {
                  const n = Number(e.target.value.replace(/\D/g, "")) || 0;
                  setPort(Math.min(n, 65535));
                }}
              />
            )}
          </Field>
        </div>
        <div className={styles.grow}>
          <Field label="Health check path" hint="Empty means a TCP connect.">
            {(id) => (
              <Input
                id={id}
                mono
                placeholder="/health"
                value={path}
                onChange={(e) => setPath(e.target.value)}
              />
            )}
          </Field>
        </div>
      </div>
      <div className={styles.sliderRow}>
        <label htmlFor="docs-ready-after">App accepts connections after</label>
        <input
          id="docs-ready-after"
          type="range"
          min={0}
          max={130}
          value={readyAfter}
          onChange={(e) => setReadyAfter(Number(e.target.value))}
        />
        <span className={styles.sliderValue}>{readyAfter}s</span>
      </div>
      <div className={styles.timeline} role="log" aria-label="Simulated build log">
        {run.lines.map((l, i) => (
          <div key={i} className={styles.tick}>
            <span className={styles.at}>{l.at}s</span>
            <span
              className={
                l.text.startsWith("==> Deployment failed")
                  ? styles.fail
                  : l.text.startsWith("==>")
                    ? styles.step
                    : undefined
              }
            >
              {l.text}
            </span>
          </div>
        ))}
      </div>
    </Demo>
  );
}
