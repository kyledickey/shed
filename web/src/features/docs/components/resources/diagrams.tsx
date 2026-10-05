import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** VolumeDiagram shows successive containers mounting one volume. */
export function VolumeDiagram() {
  return (
    <Diagram
      width={800}
      height={230}
      label="Containers of successive deployments mount the same volume"
    >
      <Node x={10} y={30} w={200} title="deployment 1" sub="shed-svc-dep1 · removed" dim />
      <Node
        x={10}
        y={100}
        w={200}
        title="deployment 2"
        sub="shed-svc-dep2 · active"
        tone="grape"
        emphasis
      />
      <Node x={10} y={170} w={200} title="deployment 3" sub="candidate, next" tone="grape" dim />
      <Zone x={330} y={44} w={460} h={140} label="Docker" />
      <Node
        x={350}
        y={90}
        w={420}
        title="shed-vol-<volumeID>"
        sub="mounted at the mount path"
        tone="accent"
        emphasis
      />
      <Edge
        points={[
          [210, 56],
          [270, 56],
          [270, 116],
          [350, 116],
        ]}
        dashed
      />
      <Edge
        points={[
          [210, 126],
          [350, 126],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [210, 196],
          [270, 196],
          [270, 136],
          [350, 136],
        ]}
        dashed
      />
      <Note x={350} y={214}>
        The volume outlives every container, and is removed only when you delete it.
      </Note>
    </Diagram>
  );
}

/** LimitsDiagram lists the limits shed sets on every container. */
export function LimitsDiagram() {
  const items = [
    ["CPU quota", "cpuLimit · 0 = none"],
    ["Memory", "memoryLimit · 64 MiB+"],
    ["Swap", "= memory, none extra"],
    ["Processes", "512 pids, fixed"],
    ["Log files", "10 MiB x 3 files"],
  ] as const;
  return (
    <Diagram width={800} height={110} label="Limits shed sets on every container">
      <Zone x={0} y={0} w={800} h={110} label="every app and database container" tone="grape" />
      {items.map(([title, sub], i) => (
        <Node key={title} x={12 + i * 156} y={40} w={146} title={title} sub={sub} />
      ))}
    </Diagram>
  );
}

/** StorageDiagram shows stop-first replacement and failure recovery. */
export function StorageDiagram() {
  const xs = [0, 166, 332, 498, 664];
  const forward = [
    ["Clear strays", "other containers"],
    ["Stop previous", "graceful, 30s"],
    ["Confirm stopped", "all must be idle"],
    ["Start candidate", "no alias yet"],
    ["Health + switch", "alias, routes"],
  ] as const;
  const back = [
    [498, "Remove candidate", "failure lands here"],
    [332, "Clear strays", "all but previous"],
    [166, "Restart previous", "only if confirmed"],
    [0, "Serving again", "routes unchanged"],
  ] as const;
  return (
    <Diagram width={800} height={236} label="Stop-first replacement and failure recovery">
      {forward.map(([title, sub], i) => (
        <Node key={title} x={xs[i]!} y={20} w={136} title={title} sub={sub} emphasis={i === 1} />
      ))}
      {xs.slice(0, -1).map((x) => (
        <Edge
          key={x}
          points={[
            [x + 136, 46],
            [x + 166, 46],
          ]}
        />
      ))}
      {back.map(([x, title, sub]) => (
        <Node key={title} x={x} y={150} w={136} title={title} sub={sub} dim />
      ))}
      <Edge
        points={[
          [566, 72],
          [566, 150],
        ]}
        dashed
        label="fails"
        labelAt={[590, 118]}
      />
      {[498, 332, 166].map((x) => (
        <Edge
          key={x}
          points={[
            [x, 176],
            [x - 30, 176],
          ]}
          dashed
        />
      ))}
      <Note x={0} y={122}>
        Recovery
      </Note>
      <Note x={0} y={224}>
        If the replacement cannot be confirmed removed, the previous container stays stopped.
      </Note>
    </Diagram>
  );
}
