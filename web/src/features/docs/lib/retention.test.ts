import { describe, expect, it } from "vite-plus/test";
import { retention, simulateRetention } from "./retention";

const summary = (rows: ReturnType<typeof simulateRetention>) => ({
  local: rows.filter((r) => r.local).length,
  remote: rows.filter((r) => r.remote).length,
  deleted: rows.filter((r) => r.deleted).length,
});

describe("simulateRetention", () => {
  const cases = [
    // 14 scheduled + 1 manual = 15 rows.
    {
      name: "defaults",
      p: { keepLocal: 7, keepRemote: 30, upload: true },
      want: { local: 8, remote: 15, deleted: 0 },
    },
    {
      name: "short remote",
      p: { keepLocal: 3, keepRemote: 5, upload: true },
      want: { local: 4, remote: 6, deleted: 9 },
    },
    {
      name: "remote only",
      p: { keepLocal: 0, keepRemote: 4, upload: true },
      want: { local: 1, remote: 5, deleted: 10 },
    },
    {
      name: "local only",
      p: { keepLocal: 2, keepRemote: 9, upload: false },
      want: { local: 3, remote: 0, deleted: 12 },
    },
  ];
  it.each(cases)("$name", ({ p, want }) => {
    expect(summary(simulateRetention(p))).toEqual(want);
  });

  it("never prunes the manual backup", () => {
    const rows = simulateRetention({ keepLocal: 1, keepRemote: 1, upload: true });
    expect(rows.find((r) => r.id === "manual")).toMatchObject({
      local: true,
      remote: true,
      deleted: false,
    });
  });
});

describe("retention", () => {
  it("keeps local files that are not in S3 when keepLocal is 0", () => {
    const { dropLocal } = retention(
      [
        { id: "a", scheduled: true, local: true, remote: false },
        { id: "b", scheduled: true, local: true, remote: true },
      ],
      { keepLocal: 0, keepRemote: 5, upload: true },
    );
    expect([...dropLocal]).toEqual(["b"]);
  });

  it("leaves S3 objects alone when not uploading", () => {
    const { dropRemote } = retention(
      [
        { id: "a", scheduled: true, local: true, remote: true },
        { id: "b", scheduled: true, local: true, remote: true },
      ],
      { keepLocal: 1, keepRemote: 1, upload: false },
    );
    expect(dropRemote.size).toBe(0);
  });
});
