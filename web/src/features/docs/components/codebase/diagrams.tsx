import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** Packages that control drives, below it. */
const driven = [
  { x: 230, title: "deploy", sub: "8 leaf packages" },
  { x: 342, title: "metrics", sub: "4 leaf packages" },
  { x: 454, title: "backup", sub: "3 leaf packages" },
  { x: 566, title: "variables", sub: "2 leaf packages" },
  { x: 678, title: "observability", sub: "9 leaf packages" },
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
  "firewall",
  "telemetry",
  "otlp",
  "victoria",
  "grafana",
  "onepassword",
];

/** DependencyDiagram shows which internal packages import which. */
export function DependencyDiagram() {
  return (
    <Diagram width={800} height={494} label="Which internal packages import which">
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
      <Node x={40} y={176} w={150} h={44} title="auth" sub="audit · github" />
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
          <Node x={c.x} y={176} w={104} h={44} title={c.title} sub={c.sub} />
          <Edge
            points={[
              [495, 132],
              [495, 154],
              [c.x + 52, 154],
              [c.x + 52, 176],
            ]}
          />
          <Edge
            points={[
              [c.x + 52, 220],
              [c.x + 52, 250],
            ]}
          />
        </g>
      ))}
      <Zone
        x={10}
        y={250}
        w={780}
        h={204}
        label="leaf packages · import nothing from internal/"
        tone="grass"
      />
      {leaves.map((name, i) => {
        const row = Math.floor(i / 6);
        const col = i % 6;
        return (
          <Node key={name} x={24 + col * 126} y={282 + row * 54} w={110} h={40} title={name} />
        );
      })}
      <Note x={400} y={478} anchor="middle">
        api, control, and mcp also import audit, observability, store, and other leaf packages.
      </Note>
    </Diagram>
  );
}
