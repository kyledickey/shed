import { describe, expect, it } from "vite-plus/test";
import { extOf, recoveryCommand } from "./recovery";

const base = { rawFile: "k3j9.sql.zst.age", downloadName: "postgres-20261004-030000.sql.zst" };

describe("recoveryCommand", () => {
  const cases = [
    {
      name: "encrypted raw postgres",
      o: { ...base, kind: "postgres", ext: "sql", source: "raw", encrypted: true },
      want: "age -d -i key.txt k3j9.sql.zst.age | zstd -d | psql -U postgres -d postgres",
    },
    {
      name: "plain raw mysql",
      o: {
        ...base,
        rawFile: "k3j9.sql.zst",
        kind: "mysql",
        ext: "sql",
        source: "raw",
        encrypted: false,
      },
      want: "zstd -d -c k3j9.sql.zst | mysql -uroot -p",
    },
    {
      name: "download mongo",
      o: { ...base, kind: "mongo", ext: "archive", source: "download", encrypted: true },
      want: "zstd -d -c postgres-20261004-030000.sql.zst | mongorestore --archive --drop",
    },
    {
      name: "download redis",
      o: { ...base, kind: "redis", ext: "rdb", source: "download", encrypted: false },
      want: "zstd -d -c postgres-20261004-030000.sql.zst > dump.rdb",
    },
    {
      name: "volume tar",
      o: {
        ...base,
        rawFile: "k.tar.zst",
        kind: "app",
        ext: "tar",
        source: "raw",
        encrypted: false,
      },
      want: "mkdir restore && zstd -d -c k.tar.zst | tar -x -C restore",
    },
    {
      name: "shed.db",
      o: { ...base, rawFile: "k.db.zst", kind: "shed", ext: "db", source: "raw", encrypted: false },
      want: "zstd -d -c k.db.zst > shed.db",
    },
  ] as const;
  it.each(cases)("$name", ({ o, want }) => {
    expect(recoveryCommand({ ...o })).toBe(want);
  });
});

describe("extOf", () => {
  it("reads the extension before .zst", () => {
    expect(extOf("shed-20261004-030000.db.zst")).toBe("db");
    expect(extOf("weird")).toBe("");
  });
});
