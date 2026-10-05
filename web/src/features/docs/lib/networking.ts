/** Ports of the naming and health-check rules in internal/api and internal/deploy. */

/** slug mirrors slug() in internal/api: ASCII letters and digits, runs of anything else become one hyphen. */
export function slug(name: string): string {
  let out = "";
  let hyphen = false;
  for (const c of name.toLowerCase()) {
    if ((c >= "a" && c <= "z") || (c >= "0" && c <= "9")) {
      if (hyphen && out.length > 0) out += "-";
      out += c;
      hyphen = false;
    } else {
      hyphen = true;
    }
  }
  return out;
}

/** generatedHost mirrors Server.generatedHost: <service>-<project>.<base domain>, label capped at 63 characters. */
export function generatedHost(service: string, project: string, baseDomain: string): string {
  let label = service;
  const ps = slug(project);
  if (ps !== "") label += `-${ps}`;
  if (label.length > 63) label = label.slice(0, 63).replace(/-+$/, "");
  return `${label}.${baseDomain}`;
}

/** The health check timings of the deploy pipeline (Deployer in internal/deploy). */
export const health = {
  intervalSec: 1,
  timeoutSec: 120,
  reportSec: 5,
  startupWatchSec: 3,
  recheckSec: 10,
} as const;

export type HealthInput = {
  /** Service name, which becomes the private host. */
  name: string;
  /** Container port; 0 means the service has none. */
  port: number;
  /** Health check path, or "" for a TCP connect. */
  path: string;
  /** Seconds after the container starts until the app accepts connections. */
  readyAfter: number;
};

export type HealthRun = {
  /** Build log lines the check writes, with the second each one is written at. */
  lines: { at: number; text: string }[];
  /** Whether the deployment goes on to switch traffic. */
  healthy: boolean;
};

const ADDR = "172.18.0.4";

/** goDuration formats whole seconds like Go's time.Duration, e.g. "7s" or "1m5s". */
function goDuration(sec: number): string {
  return sec < 60 ? `${sec}s` : `${Math.floor(sec / 60)}m${sec % 60}s`;
}

/**
 * simulateHealth replays checkHealth: one probe per second from the moment
 * the container starts, a "Not ready yet" line once five seconds have passed
 * since the last report, and failure when the 120 second deadline passes.
 */
export function simulateHealth(input: HealthInput): HealthRun {
  const lines: HealthRun["lines"] = [];
  const say = (at: number, text: string) => lines.push({ at, text });
  if (input.port <= 0) {
    say(0, "==> Watching the container start");
    say(0, `No port, so no health check; watching for ${health.startupWatchSec}s`);
    say(health.startupWatchSec, `Still running after ${health.startupWatchSec}s`);
    return { lines, healthy: true };
  }
  const addr = `${ADDR}:${input.port}`;
  const how = input.path ? `GET ${input.path} on` : "TCP connect to";
  say(0, `==> Waiting for ${input.path || `port ${input.port}`} to become healthy`);
  say(0, `Probing ${how} ${addr} (timeout 2m0s)`);

  const refused = input.path
    ? `Get "http://${addr}${input.path}": dial tcp ${addr}: connect: connection refused`
    : `dial tcp ${addr}: connect: connection refused`;
  let lastReport = 0;
  for (let t = 0; t < health.timeoutSec; t += health.intervalSec) {
    if (t >= input.readyAfter) {
      say(t, `Healthy after ${goDuration(t)}`);
      say(t, "==> Switching traffic");
      say(t, `Still healthy at ${addr}`);
      say(t, `Private host ${input.name} resolves to the new container`);
      return { lines, healthy: true };
    }
    if (t - lastReport >= health.reportSec) {
      lastReport = t;
      say(t, `Not ready yet: ${refused}`);
    }
  }
  say(
    health.timeoutSec,
    `==> Deployment failed: health check timed out after 2m0s (last error: ${refused})`,
  );
  return { lines, healthy: false };
}
