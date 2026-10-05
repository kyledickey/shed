import { describe, expect, it } from "vite-plus/test";
import { formatDateUTC } from "./time";

describe("formatDateUTC", () => {
  it("formats in UTC with the zone name", () => {
    const out = formatDateUTC("2026-10-05T03:00:00Z");
    expect(out).toContain("2026");
    expect(out).toContain("UTC");
  });
});
