import { Diagram, Edge, Node, Note } from "../../diagram";

const children = [
  {
    y: 20,
    title: "Deployments",
    sub: "history · one active",
    out: "Container",
    outSub: "shed-<svc>-<dep>",
  },
  {
    y: 86,
    title: "Variables",
    sub: "KEY=value, ${{ }} refs",
    out: "Environment",
    outSub: "at deploy time",
  },
  {
    y: 152,
    title: "Domains",
    sub: "custom or generated",
    out: "Caddy route",
    outSub: "host → IP:port",
  },
  {
    y: 218,
    title: "Volumes",
    sub: "survive deploys",
    out: "Docker volume",
    outSub: "shed-vol-<id>",
  },
] as const;

/** ModelDiagram shows how a project, its services, and a service's parts relate. */
export function ModelDiagram() {
  return (
    <Diagram
      width={800}
      height={310}
      label="A project has services; a service has deployments, variables, domains and volumes"
    >
      <Node x={10} y={116} w={130} title="Project" sub="unique name" tone="accent" emphasis />
      <Node x={190} y={116} w={170} title="Service" sub="name = DNS label" tone="grape" emphasis />
      <Edge
        points={[
          [140, 142],
          [190, 142],
        ]}
      />
      {children.map((c) => {
        const cy = c.y + 26;
        return (
          <g key={c.title}>
            <Node x={440} y={c.y} w={200} title={c.title} sub={c.sub} />
            <Node x={670} y={c.y} w={120} title={c.out} sub={c.outSub} tone="sky" />
            <Edge
              points={[
                [360, 142],
                [400, 142],
                [400, cy],
                [440, cy],
              ]}
            />
            <Edge
              points={[
                [640, cy],
                [670, cy],
              ]}
              dashed
            />
          </g>
        );
      })}
      <Note x={10} y={296}>
        Databases are services too: a template supplies the image, port, a volume, and generated
        variables.
      </Note>
    </Diagram>
  );
}
