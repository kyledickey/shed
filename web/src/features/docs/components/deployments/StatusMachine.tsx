import { Diagram, Edge, Node, Note } from "../../diagram";

const row = [
  { title: "queued", tone: "neutral" },
  { title: "waiting", tone: "neutral" },
  { title: "building", tone: "sunflower" },
  { title: "deploying", tone: "sky" },
  { title: "active", tone: "grass" },
  { title: "removed", tone: "neutral" },
] as const;

const w = 110;
const pitch = 138;
const x = (i: number) => 10 + i * pitch;
const cx = (i: number) => x(i) + w / 2;

/** StatusMachine shows how a deployment's status moves. */
export function StatusMachine() {
  return (
    <Diagram width={820} height={270} label="Deployment status transitions">
      {row.map((s, i) => (
        <g key={s.title}>
          <Node
            x={x(i)}
            y={40}
            w={w}
            h={44}
            title={s.title}
            tone={s.tone}
            emphasis={s.title === "active"}
          />
          {i < row.length - 1 && (
            <Edge
              points={[
                [x(i) + w, 62],
                [x(i + 1), 62],
              ]}
              flow={i < 4}
              tone={i < 4 ? "accent" : "neutral"}
            />
          )}
        </g>
      ))}
      <Edge
        points={[
          [cx(0), 40],
          [cx(0), 20],
          [cx(2), 20],
          [cx(2), 40],
        ]}
        label="no CI wait"
        dashed
      />
      <Note x={cx(5)} y={106} anchor="middle" mono>
        superseded
      </Note>

      <Node x={10} y={170} w={110} h={44} title="canceled" />
      <Edge
        points={[
          [cx(0), 84],
          [cx(0), 170],
        ]}
        labelAt={[cx(0) + 36, 132]}
        label="cancel"
        dashed
      />
      <Node x={148} y={170} w={110} h={44} title="skipped" />
      <Edge
        points={[
          [cx(1), 84],
          [cx(1), 170],
        ]}
        labelAt={[cx(1) + 40, 132]}
        label="CI red"
        dashed
      />
      <Node x={286} y={170} w={248} h={44} title="failed" tone="tomato" />
      {[2, 3].map((i) => (
        <Edge
          key={i}
          points={[
            [cx(i), 84],
            [cx(i), 170],
          ]}
          tone="tomato"
          dashed
        />
      ))}
      <Note x={10} y={248}>
        Canceled can also happen from waiting, building, and deploying. Failed from waiting is the
        60-minute CI timeout.
      </Note>
    </Diagram>
  );
}
