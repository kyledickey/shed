/** timelineLength is the number of scheduled backups in a simulated timeline. */
export const timelineLength = 14;

/** RetentionPolicy is the part of a backup policy that decides what is kept. */
export type RetentionPolicy = { keepLocal: number; keepRemote: number; upload: boolean };

/** RetentionBackup is one succeeded backup, as retention sees it. */
export type RetentionBackup = {
  id: string;
  scheduled: boolean;
  local: boolean;
  remote: boolean;
};

/**
 * retention decides which scheduled backups lose their local file and which
 * lose their S3 object. Backups come newest first. It mirrors
 * the retention rules: manual and pre-restore backups are never
 * pruned; with keepLocal 0, a file whose backup is not in S3 is kept so no
 * backup is left without a copy; objects are pruned only while uploading.
 */
export function retention(
  backups: RetentionBackup[],
  p: RetentionPolicy,
): { dropLocal: Set<string>; dropRemote: Set<string> } {
  const dropLocal = new Set<string>();
  const dropRemote = new Set<string>();
  let local = 0;
  let remote = 0;
  for (const b of backups) {
    if (!b.scheduled) continue;
    if (b.local) {
      if (local < p.keepLocal) local++;
      else if (p.keepLocal === 0 && !b.remote) continue;
      else dropLocal.add(b.id);
    }
    if (b.remote && p.upload) {
      if (remote < p.keepRemote) remote++;
      else dropRemote.add(b.id);
    }
  }
  return { dropLocal, dropRemote };
}

/** TimelineRow is one backup of the simulated timeline after retention ran. */
export type TimelineRow = {
  id: string;
  /** Days before the latest backup. */
  daysAgo: number;
  scheduled: boolean;
  /** Copies the backup had before retention ran. */
  hadLocal: boolean;
  hadRemote: boolean;
  /** The local file exists after pruning. */
  local: boolean;
  /** The S3 object exists after pruning. */
  remote: boolean;
  /** Neither copy is left, so the backup row is deleted. */
  deleted: boolean;
};

/**
 * simulateRetention runs retention over a daily schedule and a manual backup
 * taken between the third and fourth newest. Every backup starts with the
 * copies its policy would have made: a local file, unless keepLocal is 0 and
 * the upload succeeded, and an S3 object when uploading.
 */
export function simulateRetention(p: RetentionPolicy, days = timelineLength): TimelineRow[] {
  const initial: (RetentionBackup & { daysAgo: number })[] = [];
  for (let d = 0; d < days; d++) {
    initial.push({
      id: `s${d}`,
      daysAgo: d,
      scheduled: true,
      local: !(p.upload && p.keepLocal === 0),
      remote: p.upload,
    });
    if (d === 2) {
      initial.push({ id: "manual", daysAgo: 2, scheduled: false, local: true, remote: p.upload });
    }
  }
  const { dropLocal, dropRemote } = retention(initial, p);
  return initial.map((b) => {
    const local = b.local && !dropLocal.has(b.id);
    const remote = b.remote && !dropRemote.has(b.id);
    return {
      id: b.id,
      daysAgo: b.daysAgo,
      scheduled: b.scheduled,
      hadLocal: b.local,
      hadRemote: b.remote,
      local,
      remote,
      deleted: !local && !remote,
    };
  });
}
