// Fake data for the component showcase. Nothing here talks to the API.

import { useEffect, useRef, useState } from "react";
import type { LogLine, StreamState } from "../api/events";
import type { Deployment, Metrics, MetricsRange } from "../api/types";

function rand(seed: number) {
  const x = Math.sin(seed * 12.9898) * 43758.5453;
  return x - Math.floor(x);
}

const paths = [
  "/api/users",
  "/api/projects/42",
  "/healthz",
  "/api/deploy",
  "/login",
  "/static/app.js",
];
const methods = ["GET", "GET", "GET", "POST", "PATCH", "DELETE"];

/** runtimeLine returns a varied, realistic container log line for index i. */
export function runtimeLine(i: number, at = Date.now()): string {
  const ts = new Date(at).toISOString().replace("Z", "123456Z");
  const r = rand(i + 1);
  if (r < 0.32) {
    const status = r < 0.02 ? 503 : r < 0.05 ? 404 : r < 0.08 ? 302 : 200;
    const m = methods[i % methods.length]!;
    const p = paths[(i * 7) % paths.length]!;
    return `${ts} ${m} ${p} ${status} ${(r * 140 + 2).toFixed(1)}ms`;
  }
  if (r < 0.55) {
    const level = r < 0.37 ? "warn" : r < 0.39 ? "error" : r < 0.45 ? "debug" : "info";
    const msg =
      level === "error"
        ? "query failed"
        : level === "warn"
          ? "slow query"
          : level === "debug"
            ? "cache lookup"
            : "job completed";
    return `${ts} {"level":"${level}","msg":"${msg}","job":"sync-${i % 9}","duration_ms":${Math.round(r * 900)},"user_id":${1000 + (i % 37)}}`;
  }
  if (r < 0.7) {
    return `${ts} time=${new Date(at).toISOString()} level=INFO msg="worker heartbeat" queue=default pending=${i % 13}`;
  }
  if (r < 0.74) return `${ts} WARN memory usage above 80% (412MB / 512MB)`;
  if (r < 0.76) return `${ts} ERROR: connection refused (postgres.internal:5432)`;
  return `${ts} {"level":"info","msg":"request","method":"get","path":"/api/items?page=${i % 5}","status":200,"latency":"${Math.round(r * 40)}ms","ip":"10.0.0.${i % 255}"}`;
}

/** useFakeStream mimics useLogStream with lines arriving every `interval` ms. */
export function useFakeStream(seed: number, interval = 700) {
  const [lines, setLines] = useState<LogLine[]>(() => {
    const now = Date.now();
    return Array.from({ length: seed }, (_, i) => ({
      id: i,
      text: runtimeLine(i, now - (seed - i) * 1400),
    }));
  });
  const next = useRef(seed);
  const [state, setState] = useState<StreamState>("connecting");

  useEffect(() => {
    const t0 = setTimeout(() => setState("open"), 600);
    const t = setInterval(() => {
      const burst = rand(next.current) < 0.2 ? 4 : 1;
      setLines((prev) => {
        const add = Array.from({ length: burst }, () => {
          const id = next.current++;
          return { id, text: runtimeLine(id) };
        });
        return prev.concat(add).slice(-5000);
      });
    }, interval);
    return () => {
      clearTimeout(t0);
      clearInterval(t);
    };
  }, [interval]);

  return { lines, state, clear: () => setLines([]) };
}

const buildScript = [
  "==> Cloning acme/api@4f2a9c1",
  "Cloning into '/var/lib/shed/builds/4f2a9c1'...",
  "HEAD is now at 4f2a9c1 Add rate limiting to public endpoints",
  "==> Building image with Dockerfile",
  '#0 building with "default" instance using docker driver',
  "#1 [internal] load build definition from Dockerfile",
  "#1 transferring dockerfile: 612B done",
  "#1 DONE 0.0s",
  "#2 [internal] load metadata for docker.io/library/node:22-alpine",
  "#2 DONE 0.8s",
  "#3 [internal] load .dockerignore",
  "#3 DONE 0.0s",
  "#4 [base 1/2] FROM docker.io/library/node:22-alpine",
  "#4 CACHED",
  "#5 [deps 1/3] COPY package.json bun.lock ./",
  "#5 CACHED",
  "#6 [deps 2/3] RUN npm ci",
  "#6 0.512 npm WARN deprecated inflight@1.0.6: This module is not supported",
  "#6 3.104 added 412 packages, and audited 413 packages in 3s",
  "#6 3.110 found 0 vulnerabilities",
  "#6 DONE 3.4s",
  "#7 [build 1/2] COPY . .",
  "#7 DONE 0.1s",
  "#8 [build 2/2] RUN npm run build",
  "#8 0.401 > api@1.4.0 build",
  "#8 0.402 > tsc -p . && vite build",
  "#8 2.880 vite v7.1.0 building for production...",
  "#8 4.112 ✓ 128 modules transformed.",
  "#8 4.530 dist/server.js  84.21 kB │ gzip: 22.03 kB",
  "#8 DONE 4.9s",
  "#9 exporting to image",
  "#9 exporting layers 0.7s done",
  "#9 writing image sha256:91be2f0d done",
  "#9 DONE 0.8s",
  "==> Starting container",
  "Healthcheck GET /healthz → 200 (attempt 1)",
  "==> Switching traffic to api-4f2a9c1",
];

/** useFakeBuild replays a build log, one line per tick. */
export function useFakeBuild() {
  const [n, setN] = useState(6);
  useEffect(() => {
    if (n >= buildScript.length) return;
    const t = setTimeout(() => setN(n + 1), 180 + rand(n) * 420);
    return () => clearTimeout(t);
  }, [n]);
  return {
    lines: buildScript.slice(0, n).map((text, id) => ({ id, text })),
    state: (n >= buildScript.length ? "ended" : "open") as StreamState,
    restart: () => setN(1),
  };
}

const ago = (m: number) => new Date(Date.now() - m * 60_000).toISOString();

export const deployments: Deployment[] = [
  {
    id: "d7",
    serviceId: "s1",
    status: "building",
    trigger: "push",
    commitSha: "a91c03be72f1",
    commitMessage: "Cache project lookups in the request context",
    commitAuthor: "mira",
    image: "",
    error: "",
    createdAt: ago(1),
    startedAt: ago(0.8),
    finishedAt: null,
  },
  {
    id: "d6",
    serviceId: "s1",
    status: "active",
    trigger: "push",
    commitSha: "4f2a9c1d03aa",
    commitMessage: "Add rate limiting to public endpoints",
    commitAuthor: "kyle",
    image: "shed/api:4f2a9c1",
    error: "",
    createdAt: ago(42),
    startedAt: ago(42),
    finishedAt: ago(40.6),
  },
  {
    id: "d5",
    serviceId: "s1",
    status: "failed",
    trigger: "push",
    commitSha: "e03d12ff8811",
    commitMessage: "Bump node to 22 and switch to bun lockfile",
    commitAuthor: "kyle",
    image: "",
    error: "npm ci exited with code 1",
    createdAt: ago(180),
    startedAt: ago(180),
    finishedAt: ago(178.9),
  },
  {
    id: "d4",
    serviceId: "s1",
    status: "removed",
    trigger: "redeploy",
    commitSha: "7c9e00aa1234",
    commitMessage: "Fix webhook signature check for org installs",
    commitAuthor: "mira",
    image: "shed/api:7c9e00a",
    error: "",
    createdAt: ago(60 * 26),
    startedAt: ago(60 * 26),
    finishedAt: ago(60 * 26 - 2),
  },
  {
    id: "d3",
    serviceId: "s1",
    status: "canceled",
    trigger: "manual",
    commitSha: "1b2c3d4e5f60",
    commitMessage: "Try a smaller base image",
    commitAuthor: "kyle",
    image: "",
    error: "",
    createdAt: ago(60 * 50),
    startedAt: ago(60 * 50),
    finishedAt: ago(60 * 50 - 0.5),
  },
];

const ranges: Record<MetricsRange, number> = {
  "1h": 3600,
  "6h": 21600,
  "24h": 86400,
  "7d": 604800,
};
const SAMPLES = 120;

/** sampleMetrics returns plausible, deterministic metrics for a range. */
export function sampleMetrics(range: MetricsRange): Metrics {
  const step = ranges[range] / SAMPLES;
  const end = Math.floor(Date.now() / 1000 / step) * step;
  const walk = (base: number, spread: number, seed: number) => {
    let v = base;
    return Array.from({ length: SAMPLES }, (_, i) => {
      v += (rand(i * 3 + seed * 101) - 0.5) * spread * 0.6 + (base - v) * 0.12;
      const spike = rand(i + seed * 7) > 0.97 ? spread * 1.6 : 0;
      return Math.max(0, v + Math.sin((i + seed * 13) / 9) * spread * 0.35 + spike);
    });
  };
  const gap = (values: number[]) => values.map((v, i) => (i > 70 && i < 74 ? null : v));
  return {
    range,
    start: new Date((end - step * (SAMPLES - 1)) * 1000).toISOString(),
    step,
    cpuLimit: 2,
    memoryLimit: 512 * 1024 ** 2,
    cpu: gap(walk(22, 16, 1)),
    memory: gap(walk(260 * 1024 ** 2, 40 * 1024 ** 2, 2)),
    netRx: gap(walk(220_000, 160_000, 3)),
    netTx: gap(walk(80_000, 60_000, 4)),
    diskRead: gap(walk(24_000, 30_000, 5)),
    diskWrite: gap(walk(52_000, 40_000, 6)),
  };
}
