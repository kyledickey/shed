import { useState } from "react";
import { Badge } from "../../../../components/Badge";
import { Input } from "../../../../components/Form";
import { formatBytes } from "../../../../lib/format";
import { headroom, type BuildLimits, type DiskState } from "../../lib/buildHeadroom";
import { Demo } from "../../kit";
import styles from "./configuration.module.css";

const GiB = 1024 ** 3;

const diskText: Record<
  DiskState,
  { label: string; tone: "grass" | "sunflower" | "tomato" | "neutral" }
> = {
  ok: { label: "Builds start", tone: "grass" },
  refused: { label: "New builds are refused", tone: "sunflower" },
  canceled: { label: "Running builds are canceled", tone: "tomato" },
  off: { label: "Disk check is off", tone: "neutral" },
};

const defaults: BuildLimits = { memoryMB: 2048, cpus: 2, minFreeMB: 2048 };
type HostInput = { cpus: number; memoryGB: number; diskGB: number; usedGB: number };
const sampleHost: HostInput = { cpus: 4, memoryGB: 8, diskGB: 80, usedGB: 74 };

function Stat({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div className={styles.stat}>
      <span className={styles.statLabel}>{label}</span>
      <span className={styles.statValue}>{value}</span>
      {detail && <span className={styles.statDetail}>{detail}</span>}
    </div>
  );
}

/** BuildLimitsDemo compares [build] limits with a host size the reader types. */
export function BuildLimitsDemo() {
  const [limits, setLimits] = useState(defaults);
  const [host, setHost] = useState(sampleHost);
  const r = headroom(
    {
      cpus: Math.max(1, host.cpus),
      memoryTotal: Math.max(1, host.memoryGB) * GiB,
      diskTotal: host.diskGB * GiB,
      diskUsed: host.usedGB * GiB,
    },
    limits,
  );
  const percent = (share: number | null) =>
    share === null ? "unlimited" : `${Math.round(share * 100)}% of the host`;
  const num = (label: string, value: number, set: (n: number) => void, step = 1) => (
    <label className={styles.control}>
      {label}
      <Input
        type="number"
        min={0}
        step={step}
        value={value}
        onChange={(e) => set(Math.max(0, Number(e.target.value) || 0))}
      />
    </label>
  );
  return (
    <Demo title="Build limits against a host">
      <div className={styles.stack}>
        <div className={styles.controls}>
          {num("Host CPUs", host.cpus, (cpus) => setHost({ ...host, cpus }))}
          {num("Host memory (GiB)", host.memoryGB, (memoryGB) => setHost({ ...host, memoryGB }))}
          {num("Disk size (GiB)", host.diskGB, (diskGB) => setHost({ ...host, diskGB }))}
          {num("Disk used (GiB)", host.usedGB, (usedGB) => setHost({ ...host, usedGB }))}
        </div>
        <div className={styles.controls}>
          {num("build.memory_mb", limits.memoryMB, (memoryMB) =>
            setLimits({ ...limits, memoryMB }),
          )}
          {num("build.cpus", limits.cpus, (cpus) => setLimits({ ...limits, cpus }), 0.5)}
          {num("build.min_free_mb", limits.minFreeMB, (minFreeMB) =>
            setLimits({ ...limits, minFreeMB }),
          )}
        </div>
        <div className={styles.stats}>
          <Stat
            label="Builder memory"
            value={limits.memoryMB ? `${limits.memoryMB} MiB` : "none"}
            detail={percent(r.memoryShare)}
          />
          <Stat
            label="Builder CPU"
            value={limits.cpus ? `${limits.cpus} cores` : "none"}
            detail={percent(r.cpuShare)}
          />
          <Stat
            label="Free disk"
            value={`${r.freeMB} MiB`}
            detail={`${formatBytes(host.diskGB * GiB)} total`}
          />
        </div>
        <Badge tone={diskText[r.disk].tone}>{diskText[r.disk].label}</Badge>
      </div>
    </Demo>
  );
}
