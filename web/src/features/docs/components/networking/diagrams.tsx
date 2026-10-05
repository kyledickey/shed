import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** NetworkDiagram shows containers and Caddy on one project network. */
export function NetworkDiagram() {
  return (
    <Diagram width={800} height={300} label="A project network with containers and Caddy">
      <Zone x={200} y={40} w={590} h={250} label="network shed-<projectID>" tone="sky" />
      <Node x={10} y={140} w={140} title="Caddy" sub="in the shed process" tone="accent" emphasis />
      <Node x={220} y={84} w={210} title="web" sub="alias web · 172.18.0.4" tone="grape" />
      <Node x={560} y={84} w={210} title="postgres" sub="alias postgres · 172.18.0.2" />
      <Node
        x={220}
        y={204}
        w={210}
        title="web, next deployment"
        sub="no alias until healthy"
        tone="grape"
        dim
      />
      <Node x={560} y={204} w={210} title="redis" sub="alias redis · 172.18.0.3" />
      <Edge
        points={[
          [150, 166],
          [180, 166],
          [180, 110],
          [220, 110],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [430, 110],
          [560, 110],
        ]}
        label="postgres:5432"
        dashed
      />
      <Edge
        points={[
          [430, 230],
          [560, 230],
        ]}
        label="redis:6379"
        dashed
      />
      <Note x={10} y={218}>
        Container name
      </Note>
      <Note x={10} y={236} mono>
        shed-&lt;serviceID&gt;-
      </Note>
      <Note x={10} y={252} mono>
        &lt;deploymentID&gt;
      </Note>
    </Diagram>
  );
}

/** ProxyDiagram shows how Caddy picks a route from the Host header. */
export function ProxyDiagram() {
  return (
    <Diagram width={800} height={290} label="How Caddy picks a route from the Host header">
      <Node x={10} y={124} w={150} title="Request" sub="Host header" />
      <Node
        x={210}
        y={112}
        w={160}
        h={76}
        title="Caddy"
        sub="server shed · :443"
        tone="accent"
        emphasis
      />
      <Node
        x={470}
        y={24}
        w={320}
        title="shed.example.com"
        sub="to 127.0.0.1:3000 (server.listen)"
        tone="accent"
      />
      <Node
        x={470}
        y={104}
        w={320}
        title="app.example.com"
        sub="to 172.18.0.4:8080 (active container)"
        tone="grape"
      />
      <Node x={470} y={184} w={320} title="any other host" sub="static_response 404" dim />
      <Edge
        points={[
          [160, 150],
          [210, 150],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 50],
          [470, 50],
        ]}
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 130],
          [470, 130],
        ]}
      />
      <Edge
        points={[
          [370, 150],
          [420, 150],
          [420, 210],
          [470, 210],
        ]}
        dashed
      />
      <Note x={20} y={268}>
        Routes are sorted by host and each one is terminal; the 404 handler is last.
      </Note>
    </Diagram>
  );
}

/** HealthDiagram shows the health check steps around the alias change. */
export function HealthDiagram() {
  const xs = [0, 166, 332, 498, 664];
  const steps = [
    ["Start", "no alias yet"],
    ["Probe", "1s tick · 2m max"],
    ["Add alias", "same IP"],
    ["Probe again", "10s max"],
    ["Route + retire", "old one stops"],
  ] as const;
  return (
    <Diagram width={800} height={120} label="Health check steps before traffic switches">
      {steps.map(([title, sub], i) => (
        <Node
          key={title}
          x={xs[i]!}
          y={20}
          w={136}
          title={title}
          sub={sub}
          emphasis={i === 1 || i === 3}
        />
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
      <Note x={0} y={104}>
        Until the last step, the previous container keeps the alias and the routes.
      </Note>
    </Diagram>
  );
}
