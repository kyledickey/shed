import { describe, expect, it } from "vite-plus/test";
import { archiveFile, archiveName, objectKey, planBackup } from "./backupPlan";

describe("planBackup", () => {
  const cases = [
    ["postgres", true, "dump", "sql"],
    ["mysql", true, "dump", "sql"],
    ["mongo", true, "dump", "archive"],
    ["redis", true, "dump", "rdb"],
    ["postgres", false, "volume", "tar"],
    ["redis", false, "volume", "tar"],
    ["app", true, "volume", "tar"],
    ["app", false, "volume", "tar"],
  ] as const;
  it.each(cases)("%s running=%s -> %s .%s", (kind, running, method, ext) => {
    expect(planBackup(kind, running)).toMatchObject({ method, ext });
  });
});

describe("archiveName", () => {
  it("adds .age only when encrypted", () => {
    expect(archiveName("abc", "sql", false)).toBe("abc.sql.zst");
    expect(archiveName("abc", "sql", true)).toBe("abc.sql.zst.age");
  });
});

describe("archiveFile", () => {
  it("rebuilds the stored name from the download name", () => {
    const b = { id: "k3j9", fileName: "postgres-20261004-030000.sql.zst", encrypted: true };
    expect(archiveFile(b)).toBe("k3j9.sql.zst.age");
    expect(archiveFile({ ...b, encrypted: false })).toBe("k3j9.sql.zst");
  });
});

describe("objectKey", () => {
  const cases = [
    ["backups", "svc1", "a.tar.zst", "backups/services/svc1/a.tar.zst"],
    ["", "svc1", "a.tar.zst", "services/svc1/a.tar.zst"],
    ["/nested/prefix/", null, "a.db.zst", "nested/prefix/system/a.db.zst"],
    ["", null, "a.db.zst", "system/a.db.zst"],
  ] as const;
  it.each(cases)("prefix %j service %j", (prefix, id, file, want) => {
    expect(objectKey(prefix, id, file)).toBe(want);
  });
});
