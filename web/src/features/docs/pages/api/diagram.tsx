import { Diagram, Edge, Node } from "../../diagram";

/** RoutingDiagram shows which family of routes a request to shed lands on. */
export function RoutingDiagram() {
  const targets = [
    { y: 20, title: "Public routes", sub: "auth · setup · webhook" },
    { y: 90, title: "Session routes", sub: "401 without shed_session", accent: true },
    { y: 160, title: "Unknown /api/*", sub: '404 {"error":"not found"}' },
    { y: 230, title: "Dashboard (SPA)", sub: "GET /* → index.html" },
  ];
  return (
    <Diagram width={800} height={300} label="How requests are routed inside shed's HTTP handler">
      <Node x={20} y={125} w={130} title="Your client" sub="browser · curl" />
      <Node x={240} y={125} w={150} title="Router" sub="method + path match" />
      <Edge
        points={[
          [150, 151],
          [240, 151],
        ]}
        flow
        tone="accent"
      />
      {targets.map((t) => (
        <g key={t.title}>
          <Node
            x={500}
            y={t.y}
            w={280}
            title={t.title}
            sub={t.sub}
            tone={t.accent ? "accent" : "neutral"}
            emphasis={t.accent}
          />
          <Edge
            points={[
              [390, 151],
              [445, 151],
              [445, t.y + 26],
              [500, t.y + 26],
            ]}
            flow={t.accent}
            tone={t.accent ? "accent" : "neutral"}
          />
        </g>
      ))}
    </Diagram>
  );
}
