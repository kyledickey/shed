import { describe, expect, it } from "vite-plus/test";
import { auditPath } from "./audit";

describe("auditPath", () => {
  it("sends only the filters that are set", () => {
    expect(auditPath({ actor: "", action: "service", service: "", outcome: "failed" }, null)).toBe(
      "/audit?limit=100&action=service&outcome=failed",
    );
  });

  it("asks for events before the last one", () => {
    expect(auditPath({ actor: "octocat", action: "", service: "s1", outcome: "" }, 42)).toBe(
      "/audit?limit=100&actor=octocat&service=s1&before=42",
    );
  });
});
