import { Diagram, Edge, Node, Zone } from "../../diagram";

/** LayerDiagram shows how configuration sources stack. */
export function LayerDiagram() {
  const x = [40, 230, 420, 610];
  return (
    <Diagram
      width={800}
      height={100}
      label="Configuration layers: defaults, file, environment, validation"
    >
      <Node x={x[0]!} y={24} w={150} title="Defaults" sub="config.defaults()" />
      <Node x={x[1]!} y={24} w={150} title="shed.toml" sub="missing file is fine" />
      <Node
        x={x[2]!}
        y={24}
        w={150}
        title="SHED_* env"
        sub="wins over the file"
        tone="accent"
        emphasis
      />
      <Node x={x[3]!} y={24} w={150} title="Validate" sub="bad value stops boot" />
      {[0, 1, 2].map((i) => (
        <Edge
          key={i}
          points={[
            [x[i]! + 150, 50],
            [x[i + 1]!, 50],
          ]}
          flow
          tone="accent"
        />
      ))}
    </Diagram>
  );
}

/** DataDirDiagram maps shed's files to the directories that hold them. */
export function DataDirDiagram() {
  return (
    <Diagram width={800} height={350} label="Where shed keeps its files">
      <Zone x={10} y={10} w={250} h={214} label="next to -config (/etc/shed)" />
      <Node x={30} y={40} w={210} h={44} title="shed.toml" sub="your settings" />
      <Node x={30} y={100} w={210} h={44} title="shed.log" sub="app log, rotated" />
      <Node x={30} y={160} w={210} h={44} title="caddy.log" sub="embedded Caddy's log" />

      <Zone x={10} y={244} w={250} h={96} label="Docker root, not data.dir" />
      <Node x={30} y={280} w={210} h={44} title="shed-vol-<volumeID>" sub="named volumes" />

      <Zone x={290} y={10} w={500} h={330} label="data.dir (default /var/lib/shed)" tone="accent" />
      <Node
        x={310}
        y={40}
        w={460}
        h={44}
        title="shed.db"
        sub="SQLite, WAL mode: plus shed.db-wal and shed.db-shm"
        emphasis
        tone="accent"
      />
      <Node x={310} y={100} w={460} h={44} title="caddy/" sub="TLS certificates and ACME state" />
      <Node
        x={310}
        y={160}
        w={460}
        h={44}
        title="builds/<deploymentID>/"
        sub="build workspace, deleted afterwards"
      />
      <Node
        x={310}
        y={220}
        w={460}
        h={44}
        title="logs/<deploymentID>.log"
        sub="build log, capped by deployments.log_max_mb"
      />
      <Node
        x={310}
        y={280}
        w={460}
        h={44}
        title="backups/<serviceID|system>/"
        sub="<id>.<ext>.zst[.age] and *.partial files"
      />
    </Diagram>
  );
}
