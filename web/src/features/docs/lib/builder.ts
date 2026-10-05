/** cpuPeriod is the CFS period, in microseconds, of the builder's CPU quota. */
export const cpuPeriod = 100_000;

/** The shed builder's name, and the container buildx names after it. */
export const builderName = "shed";

/**
 * builderCreateCommand renders the `docker buildx create` command shed runs
 * for the given limits. Zero means unlimited and omits the option.
 */
export function builderCreateCommand(memoryMB: number, cpus: number): string {
  const args = [
    "docker buildx create",
    `--name ${builderName}`,
    "--driver docker-container",
    "--bootstrap",
  ];
  if (memoryMB > 0) {
    const bytes = memoryMB * 1024 * 1024;
    args.push(`--driver-opt memory=${bytes}`, `--driver-opt memory-swap=${bytes}`);
  }
  if (cpus > 0) {
    const quota = Math.max(Math.trunc(cpus * cpuPeriod), 1000);
    args.push(`--driver-opt cpu-period=${cpuPeriod}`, `--driver-opt cpu-quota=${quota}`);
  }
  return args.join(" \\\n  ");
}

/** DiskGuard is the free-space limits that follow from build.min_free_mb. */
export type DiskGuard = {
  /** A build refuses to start below this many bytes free. */
  startBelow: number;
  /** A running build is canceled below this many bytes free. */
  cancelBelow: number;
};

/** diskGuard computes the guard's two thresholds. Zero disables the guard. */
export function diskGuard(minFreeMB: number): DiskGuard {
  const bytes = minFreeMB * 1024 * 1024;
  return { startBelow: bytes, cancelBelow: Math.floor(bytes / 2) };
}

/** guardVerdict says what the guard would do to a build given free bytes. */
export function guardVerdict(free: number, g: DiskGuard): "ok" | "refuse" | "cancel" {
  if (g.startBelow === 0) return "ok";
  if (free < g.cancelBelow) return "cancel";
  return free < g.startBelow ? "refuse" : "ok";
}
