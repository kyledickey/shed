import { describe, expect, it } from "vite-plus/test";
import { validateSettings } from "../features/telemetry/TelemetryPage";
import { deploymentOverviewUrl, projectFolderUrl, settling } from "./telemetry";
import type { ServiceTelemetry, Telemetry, TelemetryComponent, TelemetrySettings } from "./types";

const settings: TelemetrySettings = {
  enabled: true,
  metricsRetentionDays: 30,
  logsRetentionDays: 14,
  tracesRetentionDays: 14,
  diskLimitGB: 20,
  defaultCaps: { spansPerSecond: 5000, logsPerSecond: 2000, series: 50000 },
};

const telemetry = (enabled: boolean, states: TelemetryComponent["state"][]): Telemetry => ({
  settings: { ...settings, enabled },
  components: states.map((state) => ({
    name: "metrics",
    state,
    version: "v1",
    error: "",
    cpuPercent: null,
    memoryBytes: null,
    memoryLimit: 1,
    diskBytes: null,
  })),
  ingest: { spans: 0, logs: 0, points: 0 },
  droppedLastHour: [],
  firingAlerts: [],
  grafanaUrl: "",
  otlpEndpoint: "",
});

describe("settling", () => {
  it("polls while components move toward the settings", () => {
    expect(settling(telemetry(true, ["running", "starting"]))).toBe(true);
    expect(settling(telemetry(false, ["running", "stopped"]))).toBe(true);
  });

  it("stops polling once they arrive or fail", () => {
    expect(settling(telemetry(true, ["running", "failed"]))).toBe(false);
    expect(settling(telemetry(false, ["stopped"]))).toBe(false);
    expect(settling(telemetry(false, []))).toBe(false);
  });
});

describe("validateSettings", () => {
  it("accepts the defaults", () => {
    expect(validateSettings(settings)).toBeNull();
  });

  it.each([
    [{ logsRetentionDays: 0 }, "Retention"],
    [{ metricsRetentionDays: 3651 }, "Retention"],
    [{ tracesRetentionDays: Number.NaN }, "Retention"],
    [{ diskLimitGB: 0 }, "disk limit"],
    [{ defaultCaps: { ...settings.defaultCaps, series: 0 } }, "cap"],
  ])("rejects %o", (change, problem) => {
    expect(validateSettings({ ...settings, ...change })).toContain(problem);
  });
});

describe("Grafana links", () => {
  const grafana = "https://shed.example.com/grafana/";
  const service: ServiceTelemetry = {
    caps: { spansPerSecond: 5000, logsPerSecond: 2000, series: 50000 },
    overridden: false,
    dropping: false,
    folderUrl: `${grafana}dashboards/f/s-svc1/`,
    dashboards: [
      {
        uid: "svc-overview-svc1",
        title: "Overview",
        url: `${grafana}d/svc-overview-svc1`,
        customized: false,
        outdated: false,
      },
      {
        uid: "svc-logs-svc1",
        title: "Logs",
        url: `${grafana}d/svc-logs-svc1`,
        customized: false,
        outdated: false,
      },
    ],
    firingAlerts: [],
  };
  const t = (iso: string) => Date.parse(iso);
  const now = t("2026-10-27T13:00:00Z");

  it("links a project to its folder", () => {
    expect(projectFolderUrl(grafana, "p1")).toBe(`${grafana}dashboards/f/p-p1/`);
  });

  it("opens the Overview 15 minutes either side of a finished deployment", () => {
    const url = deploymentOverviewUrl(
      service,
      {
        createdAt: "2026-10-27T11:59:00Z",
        startedAt: "2026-10-27T12:00:00Z",
        finishedAt: "2026-10-27T12:05:00Z",
      },
      now,
    );
    expect(url).toBe(
      `${grafana}d/svc-overview-svc1?from=${t("2026-10-27T11:45:00Z")}&to=${t("2026-10-27T12:20:00Z")}`,
    );
  });

  it("runs to now for a deployment still going, from when it was queued", () => {
    const url = deploymentOverviewUrl(
      service,
      { createdAt: "2026-10-27T12:55:00Z", startedAt: null, finishedAt: null },
      now,
    );
    expect(url).toBe(`${grafana}d/svc-overview-svc1?from=${t("2026-10-27T12:40:00Z")}&to=${now}`);
  });

  it("has no link without an Overview dashboard", () => {
    const none = { ...service, dashboards: [] };
    expect(
      deploymentOverviewUrl(
        none,
        { createdAt: "2026-10-27T12:00:00Z", startedAt: null, finishedAt: null },
        now,
      ),
    ).toBeNull();
  });
});
