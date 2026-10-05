import { describe, expect, it } from "vite-plus/test";
import { headingText, readProgress, stepOfHeading, stepStates } from "./buildLog";

describe("headingText", () => {
  it.each([
    ["==> Cloning", "Cloning"],
    [" ==> from the container", null],
    ["Name: shed-a-b", null],
  ])("%s", (line, want) => {
    expect(headingText(line)).toBe(want);
  });
});

describe("stepOfHeading", () => {
  it.each([
    ["Waiting for CI on a1b2c3d", "ci"],
    ["CI passed", "ci"],
    ["Building acme/web@a1b2c3d", "build"],
    ["Building with Railpack", "build"],
    ["Reusing image sha256:abc", "build"],
    ["Pulling postgres:18-alpine", "build"],
    ["Starting container", "start"],
    ["Stopping previous deployment x", "start"],
    ["Waiting for port 3000 to become healthy", "health"],
    ["Waiting for /healthz to become healthy", "health"],
    ["Watching the container start", "health"],
    ["Switching traffic", "switch"],
    ["Deployment failed: boom", null],
  ])("%s", (text, want) => {
    expect(stepOfHeading(text)).toBe(want);
  });
});

describe("readProgress and stepStates", () => {
  const live = [
    "Building acme/web@a1b2c3d",
    "Cloning",
    "Building with Dockerfile",
    "Starting container",
    "Waiting for port 3000 to become healthy",
    "Switching traffic",
  ];
  it("reads a deployment that went live", () => {
    const p = readProgress(live);
    expect(p).toMatchObject({ reached: "switch", usedCi: false, ended: null });
    expect(stepStates(p, "active")).toEqual({
      ci: "unused",
      build: "done",
      start: "done",
      health: "done",
      switch: "done",
    });
  });
  it("places a failed health check", () => {
    const p = readProgress([
      "Waiting for CI on a1b2c3d",
      "CI passed",
      ...live.slice(0, 5),
      "Deployment failed: health check timed out after 2m0s",
    ]);
    expect(p.ended).toEqual({ status: "failed", reason: "health check timed out after 2m0s" });
    expect(stepStates(p, "failed")).toEqual({
      ci: "done",
      build: "done",
      start: "done",
      health: "failed",
      switch: "pending",
    });
  });
  it("places a deployment still building", () => {
    const p = readProgress(live.slice(0, 2));
    expect(stepStates(p, "building").build).toBe("current");
  });
  it("treats an empty log as not started", () => {
    const p = readProgress([]);
    expect(Object.values(stepStates(p, "queued"))).toEqual(Array(5).fill("pending"));
  });
  it("notes truncation", () => {
    expect(
      readProgress(["Log truncated: size limit reached, further output is discarded"]).truncated,
    ).toBe(true);
  });
});
