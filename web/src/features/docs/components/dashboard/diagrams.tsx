import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** DataFlowDiagram shows how data flows from the API to the screen. */
export function DataFlowDiagram() {
  return (
    <Diagram width={800} height={270} label="How data flows from the API to the screen">
      <Zone x={10} y={30} w={560} h={170} label="dashboard · web/src" tone="sky" />
      <Node x={26} y={64} w={120} title="Route" sub="loader" />
      <Node x={176} y={64} w={130} title="Query hook" sub="api/*.ts" />
      <Node x={336} y={64} w={110} title="API client" sub="client.ts" />
      <Node x={176} y={140} w={130} h={44} title="Query cache" sub="keys.ts" />
      <Node x={26} y={140} w={120} h={44} title="Component" sub="features/" />
      <Node x={620} y={64} w={170} title="shed API" sub="/api/*" tone="accent" emphasis />
      <Node x={620} y={150} w={170} title="SQLite, Docker" sub="source of truth" />

      <Edge
        points={[
          [146, 90],
          [176, 90],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [306, 90],
          [336, 90],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [446, 90],
          [620, 90],
        ]}
        label="fetch"
        flow
        tone="accent"
      />
      <Edge
        points={[
          [705, 116],
          [705, 150],
        ]}
      />
      <Edge
        points={[
          [241, 116],
          [241, 140],
        ]}
        dashed
      />
      <Edge
        points={[
          [176, 162],
          [146, 162],
        ]}
        dashed
      />
      <Note x={10} y={226}>
        The route's loader and the component use the same query options, so the page opens with
      </Note>
      <Note x={10} y={244}>
        cached data. A mutation invalidates the keys it changed, and the hooks refetch.
      </Note>
    </Diagram>
  );
}
