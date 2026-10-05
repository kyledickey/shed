import { Diagram, Edge, Node } from "../../diagram";

const roles = [
  { title: "Sign-in", sub: "OAuth · /api/auth/callback" },
  { title: "Repository access", sub: "installation token, read-only" },
  { title: "Push webhooks", sub: "push → /api/github/webhook" },
  { title: "CI status", sub: "checks + statuses: read" },
] as const;

/** AppDiagram shows the four jobs of shed's one GitHub App. */
export function AppDiagram() {
  return (
    <Diagram
      width={600}
      height={290}
      label="One GitHub App provides sign-in, repository access, push webhooks, and CI status"
    >
      <Node
        x={10}
        y={118}
        w={160}
        h={60}
        title="GitHub App"
        sub="created by shed"
        tone="accent"
        emphasis
      />
      {roles.map((r, i) => {
        const y = 20 + i * 66;
        return (
          <g key={r.title}>
            <Node x={330} y={y} w={250} h={52} title={r.title} sub={r.sub} />
            <Edge
              points={[
                [170, 148],
                [250, 148],
                [250, y + 26],
                [330, y + 26],
              ]}
              tone="accent"
            />
          </g>
        );
      })}
    </Diagram>
  );
}
