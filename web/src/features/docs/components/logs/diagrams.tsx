import { Diagram, Edge, Node, Note } from "../../diagram";

/** SourcesDiagram shows where each kind of log lives and how it reaches the browser. */
export function SourcesDiagram() {
  const lanes = [
    {
      y: 16,
      cells: [
        [150, "Build pipeline", "steps + BuildKit"],
        [130, "Redact", "service vars"],
        [210, "Log file", "<data>/logs/<id>.log"],
        [230, "Build log stream", "/api/deployments/{id}/logs"],
      ],
    },
    {
      y: 96,
      cells: [
        [150, "Container", "stdout + stderr"],
        [130, "json-file", "10 MiB x 3 files"],
        [210, "Redact", "stored vars"],
        [230, "Runtime log stream", "/api/services/{id}/logs"],
      ],
    },
    {
      y: 176,
      cells: [
        [150, "shed itself", "structured text"],
        [130, "Fan-out", "three sinks"],
        [210, "Ring buffer", "+ stderr, shed.log"],
        [230, "shed log stream", "/api/logs"],
      ],
    },
  ] as const;
  return (
    <Diagram
      width={800}
      height={250}
      label="Sources, storage, and streams of the three kinds of logs"
    >
      {lanes.map((lane) => {
        let x = 0;
        return (
          <g key={lane.y}>
            {lane.cells.map(([w, title, sub], i) => {
              const at = x;
              x += w + 26;
              return (
                <g key={title}>
                  <Node
                    x={at}
                    y={lane.y}
                    w={w}
                    title={title}
                    sub={sub}
                    tone={i === 3 ? "accent" : "neutral"}
                  />
                  {i < 3 && (
                    <Edge
                      points={[
                        [at + w, lane.y + 26],
                        [at + w + 26, lane.y + 26],
                      ]}
                    />
                  )}
                </g>
              );
            })}
          </g>
        );
      })}
      <Note x={0} y={244}>
        Docker's own copy of container output is never redacted, and neither is shed's log.
      </Note>
    </Diagram>
  );
}

/** SseDiagram shows a build log stream between the browser and the API. */
export function SseDiagram() {
  const rows: [string, boolean, number][] = [
    ["GET /api/deployments/{id}/logs", true, 40],
    ['event: status  {"status":"building"}', false, 90],
    ["event: log  (replay of the file so far)", false, 140],
    ["event: log  (new lines, polled every 500 ms)", false, 190],
    ['event: status  {"status":"active"}', false, 240],
    ["event: end", false, 290],
  ];
  return (
    <Diagram width={800} height={330} label="A build log stream between the browser and the API">
      <Node x={10} y={10} w={160} h={310} title="Browser" sub="EventSource" />
      <Node x={630} y={10} w={160} h={310} title="shed API" sub="SSE endpoint" tone="accent" />
      {rows.map(([label, request, y]) => (
        <Edge
          key={label}
          points={
            request
              ? [
                  [170, y],
                  [630, y],
                ]
              : [
                  [630, y],
                  [170, y],
                ]
          }
          label={label}
          tone={request ? "neutral" : "accent"}
          flow={!request && label.includes("new lines")}
        />
      ))}
    </Diagram>
  );
}
