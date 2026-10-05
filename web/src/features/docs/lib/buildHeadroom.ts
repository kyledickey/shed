const MiB = 1024 * 1024;

/** BuildLimits are the [build] settings, in the units of shed.toml. */
export type BuildLimits = { memoryMB: number; cpus: number; minFreeMB: number };

/** Host is what the host metrics endpoint reports. */
export type Host = { cpus: number; memoryTotal: number; diskTotal: number; diskUsed: number };

/**
 * DiskState is what shed does with builds at a given free space: "ok" runs
 * them, "refused" rejects new builds but lets a running one finish, and
 * "canceled" also kills a running build. "off" is min_free_mb = 0.
 */
export type DiskState = "off" | "ok" | "refused" | "canceled";

/** Headroom compares build limits with a host. */
export type Headroom = {
  /** The builder's memory cap as a share of host memory; null when unlimited. */
  memoryShare: number | null;
  /** The builder's CPU cap as a share of host cores; null when unlimited. */
  cpuShare: number | null;
  freeMB: number;
  disk: DiskState;
};

/**
 * headroom applies the build rules: a build starts only with at
 * least min_free_mb free, and a running build is canceled below half of it.
 */
export function headroom(host: Host, limits: BuildLimits): Headroom {
  const freeMB = Math.max(0, Math.floor((host.diskTotal - host.diskUsed) / MiB));
  let disk: DiskState = "ok";
  if (limits.minFreeMB === 0) disk = "off";
  else if (freeMB < limits.minFreeMB / 2) disk = "canceled";
  else if (freeMB < limits.minFreeMB) disk = "refused";
  return {
    memoryShare: limits.memoryMB > 0 ? (limits.memoryMB * MiB) / host.memoryTotal : null,
    cpuShare: limits.cpus > 0 ? limits.cpus / host.cpus : null,
    freeMB,
    disk,
  };
}
