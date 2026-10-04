const byteUnits = ["B", "KB", "MB", "GB", "TB"];

/** Formats a byte count with binary multiples, e.g. "248 MB". */
export function formatBytes(bytes: number): string {
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < byteUnits.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = value >= 100 || unit === 0 ? 0 : value >= 10 ? 1 : 2;
  return `${Number(value.toFixed(digits))} ${byteUnits[unit]}`;
}

/** Formats a byte rate, e.g. "1.2 MB/s". */
export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`;
}

/** Formats a percentage, keeping one decimal for small values. */
export function formatPercent(percent: number): string {
  return `${Number(percent.toFixed(percent < 10 ? 1 : 0))}%`;
}
