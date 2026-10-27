import { describe, expect, it } from "vite-plus/test";
import { detailPairs } from "./detail";

describe("detailPairs", () => {
  it("joins lists and drops empty ones and shown fields", () => {
    expect(
      detailPairs({ service: "web", added: ["A", "B"], changed: [], removed: ["C"], count: 3 }),
    ).toEqual([
      ["added", "A, B"],
      ["removed", "C"],
    ]);
  });

  it("writes an MCP call's arguments as key=value", () => {
    expect(
      detailPairs({
        tool: "get_service",
        arguments: { service_id: "s1", lines: 20 },
        client: "Claude",
      }),
    ).toEqual([
      ["tool", "get_service"],
      ["arguments", "service_id=s1 lines=20"],
    ]);
  });

  it("keeps scalars", () => {
    expect(detailPairs({ autoDownload: false, retentionDays: 30 })).toEqual([
      ["autoDownload", "false"],
      ["retentionDays", "30"],
    ]);
  });
});
