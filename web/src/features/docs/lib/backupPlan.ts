import type { BackupMethod, ServiceKind } from "../../../api/types";

/** BackupPlan is what a backup of a service would do in its current state. */
export type BackupPlan = {
  method: BackupMethod;
  /** Archive extension before ".zst", e.g. "sql". */
  ext: string;
  /** What produces the archive. */
  tool: string;
};

const dumps: Partial<Record<ServiceKind, BackupPlan>> = {
  postgres: { method: "dump", ext: "sql", tool: "pg_dumpall --clean --if-exists" },
  mysql: { method: "dump", ext: "sql", tool: "mysqldump --all-databases --single-transaction" },
  mongo: { method: "dump", ext: "archive", tool: "mongodump --archive" },
  redis: { method: "dump", ext: "rdb", tool: "BGSAVE, then /data/dump.rdb" },
};

const volume: BackupPlan = { method: "volume", ext: "tar", tool: "tar of the volumes" };

/** systemPlan is the plan for shed.db. */
export const systemPlan: BackupPlan = { method: "sqlite", ext: "db", tool: "VACUUM INTO" };

/**
 * planBackup mirrors how shed picks a method: a running database is dumped
 * through its own tools, a stopped database has its volumes archived, and an
 * app's volumes are always archived, live.
 */
export function planBackup(kind: ServiceKind, running: boolean): BackupPlan {
  return (running && dumps[kind]) || volume;
}

/** archiveName is the file name shed gives an archive: <id>.<ext>.zst, plus .age when encrypted. */
export function archiveName(id: string, ext: string, encrypted: boolean): string {
  return `${id}.${ext}.zst${encrypted ? ".age" : ""}`;
}

/**
 * archiveFile rebuilds the stored file name of a backup from its id and its
 * download name, which has the same extension but no ".age".
 */
export function archiveFile(b: { id: string; fileName: string; encrypted: boolean }): string {
  const dot = b.fileName.indexOf(".");
  const ext = dot < 0 ? "" : b.fileName.slice(dot);
  return `${b.id}${ext}${b.encrypted ? ".age" : ""}`;
}

/**
 * objectKey is the S3 key of an archive. A null serviceId is shed.db. Empty
 * prefixes are omitted and surrounding slashes are ignored.
 */
export function objectKey(prefix: string, serviceId: string | null, file: string): string {
  const parts = [
    prefix.replace(/^\/+|\/+$/g, ""),
    ...(serviceId ? ["services", serviceId] : ["system"]),
    file,
  ];
  return parts.filter(Boolean).join("/");
}
