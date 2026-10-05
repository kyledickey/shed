import type { Tone } from "../../../../components/tone";
import { Diagram, Edge, Node } from "../../diagram";
import { statusRules } from "../../lib/serviceStatus";

const tones: Record<string, Tone> = {
  deploying: "sky",
  stopped: "neutral",
  active: "grass",
  crashed: "tomato",
  failed: "tomato",
  offline: "neutral",
};

const labels: Record<string, string> = {
  deploying: "Deploying",
  stopped: "Stopped",
  active: "Online",
  crashed: "Crashed",
  failed: "Failed",
  offline: "Offline",
};

const step = 64;

/** StatusDiagram is the ordered list of rules that produce a service status. */
export function StatusDiagram() {
  return (
    <Diagram
      width={760}
      height={14 + step * (statusRules.length - 1) + 44 + 10}
      label="Rules that derive a service's status, checked top to bottom"
    >
      {statusRules.map((r, i) => {
        const y = 14 + i * step;
        const last = i === statusRules.length - 1;
        return (
          <g key={r.id}>
            <Node x={10} y={y} w={470} h={44} title={`${i + 1} · ${r.title}`} sub={r.detail} />
            <Node
              x={590}
              y={y}
              w={150}
              h={44}
              title={labels[r.status]!}
              tone={tones[r.status]}
              emphasis
            />
            <Edge
              points={[
                [480, y + 22],
                [590, y + 22],
              ]}
              label={last ? "always" : "yes"}
              tone={tones[r.status]}
            />
            {!last && (
              <Edge
                points={[
                  [245, y + 44],
                  [245, y + step],
                ]}
                label="no"
                labelAt={[262, y + 44 + 14 + 6]}
              />
            )}
          </g>
        );
      })}
    </Diagram>
  );
}
