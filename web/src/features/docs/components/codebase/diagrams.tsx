import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

const consumers = [
  { x: 40, title: "deploy", sub: "7 leaf packages" },
  { x: 230, title: "metrics", sub: "docker host store" },
  { x: 420, title: "backup", sub: "docker store" },
  { x: 610, title: "auth", sub: "github" },
];

const leaves = [
  "store",
  "docker",
  "build",
  "proxy",
  "github",
  "vars",
  "catalog",
  "host",
  "s3",
  "config",
  "logtail",
];

/** DependencyDiagram shows which internal packages import which. */
export function DependencyDiagram() {
  return (
    <Diagram width={800} height={440} label="Which internal packages import which">
      <Node
        x={10}
        y={8}
        w={780}
        h={44}
        title="cmd/shed"
        sub="builds each piece and injects the real implementations"
        tone="accent"
        emphasis
      />
      <Node x={320} y={88} w={160} h={44} title="api" sub="HTTP · SSE · webhook" />
      <Edge
        points={[
          [400, 52],
          [400, 88],
        ]}
        flow
        tone="accent"
      />
      {consumers.map((c) => (
        <g key={c.title}>
          <Node x={c.x} y={176} w={150} h={44} title={c.title} sub={c.sub} />
          <Edge
            points={[
              [400, 132],
              [400, 154],
              [c.x + 75, 154],
              [c.x + 75, 176],
            ]}
          />
          <Edge
            points={[
              [c.x + 75, 220],
              [c.x + 75, 250],
            ]}
          />
        </g>
      ))}
      <Zone
        x={10}
        y={250}
        w={780}
        h={150}
        label="leaf packages · import nothing from internal/"
        tone="grass"
      />
      {leaves.map((name, i) => {
        const row = i < 6 ? 0 : 1;
        const col = i < 6 ? i : i - 6;
        return (
          <Node key={name} x={24 + col * 126} y={282 + row * 54} w={110} h={40} title={name} />
        );
      })}
      <Note x={400} y={424} anchor="middle">
        api also imports store, github, logtail, and catalog directly.
      </Note>
    </Diagram>
  );
}
