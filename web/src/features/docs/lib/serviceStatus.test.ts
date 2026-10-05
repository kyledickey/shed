import { describe, expect, it } from "vite-plus/test";
import type { StatusInputs } from "./serviceStatus";
import { deriveServiceStatus } from "./serviceStatus";

describe("deriveServiceStatus", () => {
  const cases: [string, StatusInputs, string][] = [
    ["building", { latest: "building", stopped: false, active: { running: true } }, "deploying"],
    ["building beats stopped", { latest: "queued", stopped: true, active: null }, "deploying"],
    ["stopped", { latest: "active", stopped: true, active: { running: false } }, "stopped"],
    ["stopped, never deployed", { latest: null, stopped: true, active: null }, "stopped"],
    ["running", { latest: "active", stopped: false, active: { running: true } }, "active"],
    ["crashed", { latest: "active", stopped: false, active: { running: false } }, "crashed"],
    [
      "failed redeploy keeps old",
      { latest: "failed", stopped: false, active: { running: true } },
      "active",
    ],
    ["first deploy failed", { latest: "failed", stopped: false, active: null }, "failed"],
    ["canceled", { latest: "canceled", stopped: false, active: null }, "offline"],
    ["skipped", { latest: "skipped", stopped: false, active: null }, "offline"],
    ["never deployed", { latest: null, stopped: false, active: null }, "offline"],
  ];
  it.each(cases)("%s", (_, input, want) => {
    expect(deriveServiceStatus(input).status).toBe(want);
  });
});
