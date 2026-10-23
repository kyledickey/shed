import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** ReferenceDiagram shows a reference expanding in the owning service's scope. */
export function ReferenceDiagram() {
  const leaves = [
    ["POSTGRES_USER", "postgres", 68],
    ["POSTGRES_PASSWORD", "random, 24 chars", 122],
    ["POSTGRES_DB", "app", 176],
    ["SHED_PRIVATE_DOMAIN", "postgres (injected)", 230],
  ] as const;
  return (
    <Diagram
      width={800}
      height={296}
      label="web's DATABASE_URL expands through postgres's variables"
    >
      <Zone x={10} y={44} w={230} h={100} label="web" tone="grape" />
      <Zone x={290} y={44} w={500} h={240} label="postgres" tone="sky" />
      <Node
        x={25}
        y={78}
        w={200}
        title="DATABASE_URL"
        sub="${{ postgres.DATABASE_URL }}"
        tone="grape"
        emphasis
      />
      <Node
        x={305}
        y={78}
        w={200}
        title="DATABASE_URL"
        sub="postgresql://${{…}}"
        tone="sky"
        emphasis
      />
      {leaves.map(([title, sub, y]) => (
        <Node key={title} x={585} y={y} w={190} h={44} title={title} sub={sub} />
      ))}
      <Edge
        points={[
          [225, 104],
          [305, 104],
        ]}
        flow
        tone="accent"
      />
      {leaves.map(([title, , y]) => (
        <Edge
          key={title}
          points={[
            [505, 104],
            [545, 104],
            [545, y + 22],
            [585, y + 22],
          ]}
        />
      ))}
      <Note x={10} y={190}>
        References are expanded in the scope
      </Note>
      <Note x={10} y={206}>
        of the service that owns the value.
      </Note>
    </Diagram>
  );
}

/** MergeDiagram shows how a service's environment is assembled. */
export function MergeDiagram() {
  return (
    <Diagram
      width={800}
      height={96}
      label="Injected variables, 1Password Environment, stored variables, resolution, container environment"
    >
      <Node x={0} y={20} w={136} title="Injected by shed" sub="SHED_*, PORT" tone="accent" />
      <Node x={166} y={20} w={136} title="1Password" sub="if linked" />
      <Node x={332} y={20} w={136} title="Your variables" sub="win on the same key" />
      <Node x={498} y={20} w={136} title="Resolve references" sub="per owning service" emphasis />
      <Node x={664} y={20} w={136} title="Container env" sub="KEY=value, sorted" tone="grape" />
      {[136, 302, 468, 634].map((x) => (
        <Edge
          key={x}
          points={[
            [x, 46],
            [x + 30, 46],
          ]}
        />
      ))}
    </Diagram>
  );
}
