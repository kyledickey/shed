import { describe, expect, it } from "vite-plus/test";
import { ciState, type CheckRun, type CombinedStatus } from "./ci";

const done = (conclusion: CheckRun["conclusion"]): CheckRun => ({
  status: "completed",
  conclusion,
});
const running: CheckRun = { status: "in_progress", conclusion: "success" };
const noStatuses: CombinedStatus = { total: 0, state: "pending" };

describe("ciState", () => {
  const cases: [string, CheckRun[], CombinedStatus, string][] = [
    ["nothing reported", [], noStatuses, "none"],
    ["all green", [done("success"), done("neutral"), done("skipped")], noStatuses, "success"],
    ["one running", [done("success"), running], noStatuses, "pending"],
    ["failure outranks pending", [running, done("failure")], noStatuses, "failure"],
    ["cancelled counts as failure", [done("cancelled")], noStatuses, "failure"],
    ["timed out counts as failure", [done("timed_out")], noStatuses, "failure"],
    ["only a passing status", [], { total: 1, state: "success" }, "success"],
    ["pending status", [done("success")], { total: 2, state: "pending" }, "pending"],
    ["errored status", [done("success")], { total: 1, state: "error" }, "failure"],
    [
      "empty combined state is ignored",
      [done("success")],
      { total: 0, state: "pending" },
      "success",
    ],
  ];
  it.each(cases)("%s", (_, runs, combined, want) => {
    expect(ciState(runs, combined)).toBe(want);
  });
});
