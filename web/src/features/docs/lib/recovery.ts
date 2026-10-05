import type { ServiceKind } from "../../../api/types";

/** Source is where the archive file came from. */
export type Source = "download" | "raw";

/**
 * recoveryCommand is a shell pipeline that decodes an archive and feeds it to
 * the tool that reads it. A dashboard download is decrypted already; a raw
 * file from <data>/backups or S3 still carries the .age suffix when encrypted.
 * rawFile is the stored name, downloadName is the name the dashboard gives.
 */
export function recoveryCommand(opts: {
  kind: ServiceKind | "shed";
  ext: string;
  source: Source;
  encrypted: boolean;
  rawFile: string;
  downloadName: string;
}): string {
  const { kind, ext, source, encrypted, rawFile, downloadName } = opts;
  let decode: string;
  if (source === "download") decode = `zstd -d -c ${downloadName}`;
  else if (encrypted) decode = `age -d -i key.txt ${rawFile} | zstd -d`;
  else decode = `zstd -d -c ${rawFile}`;

  switch (ext) {
    case "sql":
      return kind === "mysql"
        ? `${decode} | mysql -uroot -p`
        : `${decode} | psql -U postgres -d postgres`;
    case "archive":
      return `${decode} | mongorestore --archive --drop`;
    case "rdb":
      return `${decode} > dump.rdb`;
    case "db":
      return `${decode} > shed.db`;
    default:
      return `mkdir restore && ${decode} | tar -x -C restore`;
  }
}

/** extOf returns the archive extension of a download name, e.g. "sql" for "x-2026.sql.zst". */
export function extOf(downloadName: string): string {
  const parts = downloadName.split(".");
  return parts.length >= 3 ? parts[parts.length - 2]! : "";
}
