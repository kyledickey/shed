import { describe, expect, it } from "vite-plus/test";
import { presetFor } from "./cron";

describe("presetFor", () => {
  it.each([
    ["0 * * * *", "hourly"],
    ["0 3 * * *", "daily"],
    ["0 3 * * 0", "weekly"],
    ["  0   3 * * *  ", "daily"],
    ["0 4 * * *", "custom"],
    ["CRON_TZ=Europe/Berlin 0 3 * * *", "custom"],
    ["@daily", "custom"],
    ["", "custom"],
  ])("%j is %s", (schedule, want) => {
    expect(presetFor(schedule)).toBe(want);
  });
});
