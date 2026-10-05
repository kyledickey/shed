import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

const step = (i: number) => 24 + i * 128;
const row = (i: number): [number, number][] => [
  [step(i) + 112, 98],
  [step(i + 1), 98],
];

/** PipelineDiagram shows how an archive is written, stored, and uploaded. */
export function PipelineDiagram() {
  return (
    <Diagram width={800} height={290} label="Backup archive pipeline and status transitions">
      <Zone x={14} y={36} w={516} h={104} label="written by the backup job" tone="accent" />
      <Node x={step(0)} y={72} w={112} title="Source" sub="dump|tar|VACUUM" />
      <Node x={step(1)} y={72} w={112} title="zstd" sub="streaming" />
      <Node x={step(2)} y={72} w={112} title="age" sub="optional" />
      <Node x={step(3)} y={72} w={112} title=".partial" sub="fsync · rename" />
      <Node
        x={step(4)}
        y={72}
        w={112}
        title="Local file"
        sub="<data>/backups"
        tone="accent"
        emphasis
      />
      <Node x={step(5)} y={72} w={112} title="S3 object" sub="multipart" />
      {[0, 1, 2, 3, 4].map((i) => (
        <Edge key={i} points={row(i)} flow={i < 4} tone={i < 4 ? "accent" : "neutral"} />
      ))}
      <Note x={step(5) + 56} y={60} anchor="middle" mono>
        if upload is on
      </Note>

      <Node x={24} y={190} w={150} h={44} title="queued" />
      <Node x={210} y={190} w={150} h={44} title="running" />
      <Node x={396} y={190} w={150} h={44} title="uploading" />
      <Node x={582} y={190} w={150} h={44} title="succeeded" tone="accent" emphasis />
      <Edge
        points={[
          [174, 212],
          [210, 212],
        ]}
      />
      <Edge
        points={[
          [360, 212],
          [396, 212],
        ]}
      />
      <Edge
        points={[
          [546, 212],
          [582, 212],
        ]}
      />
      <Edge
        points={[
          [285, 190],
          [285, 166],
          [657, 166],
          [657, 190],
        ]}
        dashed
        label="nothing to upload"
      />
      <Note x={24} y={266}>
        An upload failure still ends succeeded, with remote_error set. Any other failure ends
        failed.
      </Note>
    </Diagram>
  );
}
