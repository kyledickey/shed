import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** Packages that control drives, below it. */
const driven = [
  { x: 230, title: "deploy", sub: "7 leaf packages" },
  { x: 420, title: "metrics", sub: "docker host store" },
  { x: 610, title: "backup", sub: "docker store" },
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
  "update",
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
      <Node x={40} y={88} w={150} h={44} title="api" sub="HTTP · SSE · webhook" />
      <Node x={420} y={88} w={150} h={44} title="control" sub="operations and rules" />
      <Node x={610} y={88} w={150} h={44} title="mcp" sub="read-only agent tools" />
      <Edge
        points={[
          [685, 52],
          [685, 88],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [610, 110],
          [570, 110],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [115, 52],
          [115, 88],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [190, 110],
          [420, 110],
        ]}
        flow
        tone="accent"
      />
      <Node x={40} y={176} w={150} h={44} title="auth" sub="github" />
      <Edge
        points={[
          [115, 132],
          [115, 176],
        ]}
      />
      <Edge
        points={[
          [115, 220],
          [115, 250],
        ]}
      />
      {driven.map((c) => (
        <g key={c.title}>
          <Node x={c.x} y={176} w={150} h={44} title={c.title} sub={c.sub} />
          <Edge
            points={[
              [495, 132],
              [495, 154],
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
        api, control, and mcp also import store, github, update, and other leaf packages directly.
      </Note>
    </Diagram>
  );
}
