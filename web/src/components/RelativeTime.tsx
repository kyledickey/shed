import { formatDate, relativeTime } from "../lib/time";

export function RelativeTime({ iso }: { iso: string }) {
  return (
    <time dateTime={iso} title={formatDate(iso)}>
      {relativeTime(iso)}
    </time>
  );
}
