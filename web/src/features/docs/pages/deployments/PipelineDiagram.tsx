import { Diagram, Edge, Node, Note } from "../../diagram";

const steps = [
  { title: "Wait for CI", sub: "poll 10s ≤ 60m", status: "waiting" },
  { title: "Build", sub: "clone · build", status: "building" },
  { title: "Start", sub: "no alias yet", status: "deploying" },
  { title: "Health check", sub: "probe ≤ 120s", status: "deploying" },
  { title: "Switch", sub: "alias · routes", status: "deploying" },
  { title: "Active", sub: "old → removed", status: "active" },
] as const;

const w = 110;
const pitch = 138;
const x = (i: number) => 10 + i * pitch;
const cx = (i: number) => x(i) + w / 2;

/** PipelineDiagram is the happy path of a deployment with its failure exits. */
export function PipelineDiagram() {
  return (
    <Diagram
      width={820}
      height={312}
      label="Deploy pipeline: wait for CI, build, start, health check, switch, active; failures end as failed or skipped"
    >
      {steps.map((s, i) => (
        <g key={s.title}>
          <Note x={cx(i)} y={36} anchor="middle" mono>
            {s.status}
          </Note>
          <Node
            x={x(i)}
            y={46}
            w={w}
            h={56}
            title={s.title}
            sub={s.sub}
            tone={i === 5 ? "grass" : i === 0 ? "neutral" : "accent"}
            emphasis={i === 5}
            dim={i === 0}
          />
          {i < steps.length - 1 && (
            <Edge
              points={[
                [x(i) + w, 74],
                [x(i + 1), 74],
              ]}
              flow
              tone="accent"
            />
          )}
        </g>
      ))}

      <Node x={10} y={186} w={110} title="Skipped" sub="CI failed" />
      <Edge
        points={[
          [cx(0), 102],
          [cx(0), 186],
        ]}
        labelAt={[cx(0) + 34, 148]}
        label="red CI"
      />

      <Node
        x={190}
        y={186}
        w={560}
        title="Failed"
        sub="error recorded · candidate removed · previous container keeps serving"
        tone="tomato"
      />
      {[1, 2, 3, 4].map((i) => (
        <Edge
          key={i}
          points={[
            [cx(i), 102],
            [cx(i), 186],
          ]}
          tone="tomato"
          dashed
        />
      ))}

      <Note x={10} y={264}>
        A CI wait that outlasts 60 minutes fails the deployment.
      </Note>
      <Note x={10} y={280}>
        A newer deployment, the Cancel button, or stopping the service cancels it from any step.
      </Note>
      <Note x={10} y={296}>
        Services that don't wait for CI skip the first step.
      </Note>
    </Diagram>
  );
}
