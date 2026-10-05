import type { ReactNode } from "react";
import styles from "./deployments.module.css";

type Part = { lines: string[]; note: ReactNode };

const parts: Part[] = [
  {
    lines: ["==> Waiting for CI on a1b2c3d", "==> CI passed"],
    note: (
      <>
        Only for apps with <code>waitForCi</code> and a commit. Status is <code>waiting</code>.
        Errors reaching GitHub print as <code>Checking CI status: …</code> and polling continues.
      </>
    ),
  },
  {
    lines: ["==> Building acme/web@a1b2c3d"],
    note: (
      <>
        Status becomes <code>building</code>. Image apps and databases print{" "}
        <code>==&gt; Pulling &lt;image&gt;</code> instead, and a redeploy prints{" "}
        <code>==&gt; Reusing image &lt;ref&gt;</code> and skips ahead.
      </>
    ),
  },
  {
    lines: [
      "==> Cloning",
      "[git init, fetch --depth 1, checkout]",
      "==> Building with Dockerfile",
      "[docker buildx build output]",
    ],
    note: (
      <>
        Printed by the builder. The second heading is <code>==&gt; Building with Railpack</code>{" "}
        when there is no Dockerfile. Tool output follows each heading, with variable values masked.
      </>
    ),
  },
  {
    lines: ["==> Detected port 3000 from the image"],
    note: "Only when the service's port is 0. The lowest exposed TCP port is saved on the service.",
  },
  {
    lines: [
      "==> Stopping previous deployment x5k2m7q1d9ab",
      "Volumes and published ports cannot be shared, so the previous container stops first",
    ],
    note: "Only for services with volumes or a public port. Status is already deploying.",
  },
  {
    lines: [
      "==> Starting container",
      "Name: shed-p4n8…-r7c1…",
      "Image: shed/p4n8…:r7c1…",
      "Network: shed-k2…, private address web:3000 once healthy",
      "Volume: shed-vol-t9… → /data",
      "Environment: 9 variables",
      "Started container 3f9a1c2b7d10",
    ],
    note: "What the container runs with. Variables are counted, never listed. A start command and published ports add lines of their own.",
  },
  {
    lines: [
      "==> Waiting for port 3000 to become healthy",
      "Probing TCP connect to 172.18.0.4:3000 (timeout 2m0s)",
      "Not ready yet: dial tcp 172.18.0.4:3000: connect: connection refused",
      "Listening on :3000",
      "Healthy after 2.3s",
    ],
    note: (
      <>
        With a health check path the heading is <code>Waiting for /healthz to become healthy</code>{" "}
        and the probe is a <code>GET</code>. The <em>Not ready yet</em> line repeats every 5
        seconds. The container's own output is copied in (here, <code>Listening on :3000</code>),
        and the heading is the only line shed adds a space to if it starts with <code>==&gt;</code>.
      </>
    ),
  },
  {
    lines: [
      "==> Watching the container start",
      "No port, so no health check; watching for 3s",
      "Still running after 3s",
    ],
    note: "The same stage for a service without a port.",
  },
  {
    lines: [
      "==> Switching traffic",
      "Still healthy at 172.18.0.4:3000",
      "Private host web resolves to the new container",
      "Routing web.example.com → port 3000",
      "Removing previous deployment x5k2m7q1d9ab (container 0d8e1f2a3b4c)",
    ],
    note: "The alias, the second probe, then routes. Everything is written before the deployment turns active, because log followers stop then.",
  },
  {
    lines: ["==> Deployment failed: health check timed out after 2m0s (last error: …)"],
    note: (
      <>
        The last line of a deployment that ended without going live. The word after{" "}
        <em>Deployment</em> is the status: <code>failed</code>, <code>skipped</code>, or{" "}
        <code>canceled</code>.
      </>
    ),
  },
];

/** LogAnatomy is an annotated example of a deployment's build log. */
export function LogAnatomy() {
  return (
    <div className={styles.anatomy}>
      {parts.map((p) => (
        <div key={p.lines[0]} className={styles.anatomyRow}>
          <pre className={styles.anatomyLog}>
            {p.lines.map((l) => (
              <span key={l} className={l.startsWith("==> ") ? styles.logHeading : undefined}>
                {l}
                {"\n"}
              </span>
            ))}
          </pre>
          <div className={styles.anatomyNote}>{p.note}</div>
        </div>
      ))}
    </div>
  );
}
