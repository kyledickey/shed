import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** FlowDiagram shows the stages of a restore and where failures go. */
export function FlowDiagram() {
  return (
    <Diagram width={800} height={330} label="End-to-end flow of a restore">
      <Node x={20} y={40} w={200} title="1 Pre-restore backup" sub="trigger = pre-restore" />
      <Node x={300} y={40} w={200} title="2 Check the archive" sub="decrypt, decompress, scan" />
      <Node
        x={580}
        y={40}
        w={200}
        title="3 Hold the service"
        sub="deploys canceled, 409"
        tone="accent"
        emphasis
      />
      <Edge
        points={[
          [220, 66],
          [300, 66],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [500, 66],
          [580, 66],
        ]}
        flow
        tone="accent"
      />

      <Node
        x={20}
        y={150}
        w={250}
        title="restore_fences row"
        sub="retaining → replacing | loading"
      />
      <Node x={330} y={150} w={210} title="4a Volume restore" sub="retain, replace, verify" />
      <Node x={570} y={150} w={210} title="4b Dump restore" sub="psql · mysql · mongorestore" />
      <Edge
        points={[
          [270, 176],
          [330, 176],
        ]}
        dashed
      />
      <Edge
        points={[
          [640, 92],
          [640, 121],
          [435, 121],
          [435, 150],
        ]}
        label="volumes, redis"
        labelAt={[537, 121]}
      />
      <Edge
        points={[
          [720, 92],
          [720, 150],
        ]}
      />
      <Note x={20} y={222} mono>
        dump restores fence in phase loading
      </Note>

      <Node
        x={330}
        y={260}
        w={210}
        title="5 Release the hold"
        sub="start container, apply routes"
        tone="accent"
        emphasis
      />
      <Edge
        points={[
          [435, 202],
          [435, 260],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [675, 202],
          [675, 231],
          [435, 231],
          [435, 260],
        ]}
        flow
        tone="accent"
      />

      <Node
        x={20}
        y={260}
        w={250}
        title="On failure"
        sub="data put back, or stays fenced"
        tone="tomato"
      />
      <Edge
        points={[
          [360, 202],
          [360, 231],
          [145, 231],
          [145, 260],
        ]}
        dashed
        tone="tomato"
      />
    </Diagram>
  );
}

/** PhaseDiagram shows the fence phases of a volume restore and its failure paths. */
export function PhaseDiagram() {
  return (
    <Diagram
      width={800}
      height={240}
      label="Phases of a volume restore and what happens on failure"
    >
      <Zone x={6} y={30} w={312} h={100} label="phase: retaining" />
      <Zone x={326} y={30} w={308} h={100} label="phase: replacing" />
      <Node x={14} y={60} w={132} title="Fence" sub="row + stopped" />
      <Node x={170} y={60} w={132} title="Copy aside" sub="pre-restore copy" />
      <Node x={340} y={60} w={132} title="Replace" sub="empty · extract" />
      <Node x={496} y={60} w={132} title="Verify" sub="SHA-256 per file" />
      <Node x={660} y={60} w={132} title="Lift fence" sub="delete copies" tone="accent" emphasis />
      <Edge
        points={[
          [146, 86],
          [170, 86],
        ]}
      />
      <Edge
        points={[
          [302, 86],
          [340, 86],
        ]}
      />
      <Edge
        points={[
          [472, 86],
          [496, 86],
        ]}
      />
      <Edge
        points={[
          [628, 86],
          [660, 86],
        ]}
      />

      <Node x={14} y={170} w={250} title="Volumes unchanged" sub="fence lifted, service restarts" />
      <Edge
        points={[
          [240, 112],
          [240, 170],
        ]}
        dashed
        tone="tomato"
      />
      <Node x={394} y={170} w={200} title="Put back" sub="empty · copy · check" tone="tomato" />
      <Edge
        points={[
          [430, 112],
          [430, 170],
        ]}
        dashed
        tone="tomato"
      />
      <Edge
        points={[
          [560, 112],
          [560, 170],
        ]}
        dashed
        tone="tomato"
      />
      <Edge
        points={[
          [594, 196],
          [720, 196],
          [720, 112],
        ]}
        dashed
        tone="tomato"
        label="then lift"
        labelAt={[657, 196]}
      />
    </Diagram>
  );
}
