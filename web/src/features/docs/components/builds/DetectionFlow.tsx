import { Diagram, Edge, Node, Note } from "../../diagram";

/** DetectionFlow is how a repo app's build tool is chosen. */
export function DetectionFlow() {
  return (
    <Diagram
      width={900}
      height={290}
      label="Flowchart: a configured Dockerfile path must exist; otherwise a Dockerfile in the root directory is used; otherwise Railpack"
    >
      <Node x={10} y={110} w={110} title="Repo app" sub="repo @ commit" tone="grape" />
      <Node x={150} y={110} w={120} title="Clone" sub="depth 1" />
      <Node
        x={300}
        y={110}
        w={190}
        title="dockerfilePath set?"
        sub="relative to rootDir"
        tone="accent"
        emphasis
      />
      <Node
        x={300}
        y={200}
        w={190}
        title="Dockerfile in rootDir?"
        sub="after the clone"
        tone="accent"
        emphasis
      />
      <Node x={540} y={20} w={220} title="Dockerfile build" sub="docker buildx build -f" />
      <Node x={540} y={200} w={220} title="Railpack build" sub="prepare, then buildx" />
      <Node x={780} y={110} w={118} title="Image" sub="shed/<svc>:<dep>" tone="sky" emphasis />

      <Edge
        points={[
          [120, 136],
          [150, 136],
        ]}
        flow
      />
      <Edge
        points={[
          [270, 136],
          [300, 136],
        ]}
        flow
      />
      <Edge
        points={[
          [395, 110],
          [395, 46],
          [540, 46],
        ]}
        label="yes: file must exist"
        labelAt={[467, 46]}
      />
      <Edge
        points={[
          [395, 162],
          [395, 200],
        ]}
        label="no"
        labelAt={[412, 187]}
      />
      <Edge
        points={[
          [490, 214],
          [520, 214],
          [520, 62],
          [540, 62],
        ]}
      />
      <Note x={497} y={209} mono>
        yes
      </Note>
      <Edge
        points={[
          [490, 240],
          [540, 240],
        ]}
        label="no"
        labelAt={[515, 246]}
      />
      <Edge
        points={[
          [760, 46],
          [840, 46],
          [840, 110],
        ]}
      />
      <Edge
        points={[
          [760, 226],
          [840, 226],
          [840, 162],
        ]}
      />
      <Note x={10} y={280}>
        Image apps and databases skip all of this: their image is pulled as it is.
      </Note>
    </Diagram>
  );
}
