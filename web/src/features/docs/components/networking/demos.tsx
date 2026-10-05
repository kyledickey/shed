import { useState } from "react";
import { Field, Input } from "../../../../components/Form";
import { Demo, DocTable } from "../../kit";
import { generatedHost, simulateHealth } from "../../lib/networking";
import styles from "./networking.module.css";

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

/** RouteMatcher matches a typed Host header against a sample route table. */
export function RouteMatcher() {
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

/** GeneratedPreview previews the host shed generates from proxy.base_domain. */
export function GeneratedPreview() {
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

/** HealthSimulator replays a health check against a simulated container. */
export function HealthSimulator() {
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
