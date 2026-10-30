import { describe, expect, it } from "vite-plus/test";
import { historyPath, traceHref } from "./logHistory";

const now = new Date("2026-05-01T12:00:00Z");

describe("historyPath", () => {
  it("sends only the filters that are set", () => {
    expect(historyPath("s1", { q: "", level: "", range: "1h" }, null, now)).toBe(
      "/services/s1/logs/history?limit=100&from=2026-05-01T11%3A00%3A00.000Z",
    );
  });

  it("encodes the search and asks for records before the cursor", () => {
    const path = historyPath(
      "s1",
      { q: 'a "b c" level:x', level: "error", range: "7d" },
      "2026-05-01T11:59:59.5Z",
      now,
    );
    const q = new URL(path, "http://x").searchParams;
    expect(q.get("q")).toBe('a "b c" level:x');
    expect(q.get("level")).toBe("error");
    expect(q.get("from")).toBe("2026-04-24T12:00:00.000Z");
    expect(q.get("before")).toBe("2026-05-01T11:59:59.5Z");
  });
});

describe("traceHref", () => {
  it("points at the redirect", () => {
    expect(traceHref("abc")).toBe("/api/telemetry/trace/abc");
  });
});
