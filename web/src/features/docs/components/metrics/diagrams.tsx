import { Diagram, Edge, Node, Note } from "../../diagram";

/** SamplingDiagram shows how container and host samples become charts. */
export function SamplingDiagram() {
  return (
    <Diagram width={800} height={250} label="How container and host samples become charts">
      <Node x={0} y={20} w={190} title="docker stats" sub="one-shot, per container" tone="sky" />
      <Node x={0} y={90} w={190} title="/proc, /sys, statfs" sub="host counters" tone="sky" />
      <Node
        x={250}
        y={55}
        w={160}
        h={70}
        title="Collector"
        sub="tick every 10s"
        tone="accent"
        emphasis
      />
      <Node x={470} y={20} w={150} title="rates" sub="Δ counters / Δ time" />
      <Node x={470} y={90} w={150} title="sum per service" sub="overlap on deploy" />
      <Node x={660} y={55} w={140} h={70} title="SQLite" sub="7 days kept" />
      <Edge
        points={[
          [190, 46],
          [220, 46],
          [220, 80],
          [250, 80],
        ]}
      />
      <Edge
        points={[
          [190, 116],
          [220, 116],
          [220, 100],
          [250, 100],
        ]}
      />
      <Edge
        points={[
          [410, 90],
          [440, 90],
          [440, 46],
          [470, 46],
        ]}
      />
      <Edge
        points={[
          [545, 72],
          [545, 90],
        ]}
      />
      <Edge
        points={[
          [620, 116],
          [660, 116],
        ]}
      />
      <Node x={250} y={170} w={160} title="Bucket query" sub="180 buckets" />
      <Node x={470} y={170} w={150} title="metrics API" sub="?range=1h" />
      <Node x={660} y={170} w={140} title="Chart" sub="dashboard" tone="grape" />
      <Edge
        points={[
          [730, 125],
          [730, 150],
          [330, 150],
          [330, 170],
        ]}
        dashed
      />
      <Edge
        points={[
          [410, 196],
          [470, 196],
        ]}
      />
      <Edge
        points={[
          [620, 196],
          [660, 196],
        ]}
        flow
        tone="accent"
      />
      <Note x={0} y={236}>
        A container's first reading only seeds the previous-sample cache; no row is written for it.
      </Note>
    </Diagram>
  );
}
