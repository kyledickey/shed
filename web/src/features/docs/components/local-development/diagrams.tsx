import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** DevDiagram shows the local development setup. */
export function DevDiagram() {
  return (
    <Diagram width={800} height={260} label="Local development setup">
      <Zone x={170} y={30} w={170} h={110} label="bun run dev" tone="sky" />
      <Zone x={380} y={30} w={410} h={220} label="make dev · one Go process" tone="accent" />

      <Node x={10} y={74} w={130} title="Browser" sub="origin = server.url" />
      <Node x={186} y={74} w={138} title="Vite+" sub=":5173 · HMR" tone="sky" />
      <Node x={400} y={74} w={150} title="API" sub="127.0.0.1:3000" emphasis tone="accent" />
      <Node x={400} y={170} w={150} title="shed.db" sub=".dev/data" />
      <Node x={600} y={74} w={170} title="Docker Engine" sub="workloads · builds" />
      <Node x={600} y={170} w={170} title="Proxy off" sub="proxy.enabled = false" dim />

      <Edge
        points={[
          [140, 100],
          [186, 100],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [324, 100],
          [400, 100],
        ]}
        label="/api"
        flow
        tone="accent"
      />
      <Edge
        points={[
          [475, 126],
          [475, 170],
        ]}
      />
      <Edge
        points={[
          [550, 100],
          [600, 100],
        ]}
      />
      <Note x={10} y={170}>
        Everything except /api is served by Vite from web/src.
      </Note>
      <Note x={10} y={188}>
        The proxy is off, so deployed apps are not routed.
      </Note>
    </Diagram>
  );
}
