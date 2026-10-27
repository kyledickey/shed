import { describe, expect, it } from "vite-plus/test";
import { validateSettings } from "../features/telemetry/TelemetryPage";
import { settling } from "./telemetry";
import type { Telemetry, TelemetryComponent, TelemetrySettings } from "./types";

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
