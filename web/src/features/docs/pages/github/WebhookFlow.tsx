import { Diagram, Edge, Node } from "../../diagram";

const gates: { title: string; sub: string; out: string; outSub: string; bad?: boolean }[] = [
  {
    title: "Signature header well-formed",
    sub: "sha256= and 64 hex characters",
    out: "401",
    outSub: "invalid signature",
    bad: true,
  },
  {
    title: "Under the capacity limits",
    sub: "4 at once · burst of 8 · 1 per second",
    out: "429",
    outSub: "webhook capacity exceeded",
    bad: true,
  },
  {
    title: "Body within 1 MiB",
    sub: "read within 10 seconds",
    out: "413",
    outSub: "webhook body too large",
    bad: true,
  },
  {
    title: "HMAC-SHA256 matches the body",
    sub: "X-Hub-Signature-256",
    out: "401",
    outSub: "invalid signature",
    bad: true,
  },
  {
    title: "A push to a branch",
    sub: "not a ping, not a deleted branch",
    out: "202",
    outSub: "acknowledged and ignored",
  },
  {
    title: "Services that track it",
    sub: "app · auto_deploy · repo@branch",
    out: "202",
    outSub: "nothing to deploy",
  },
  {
    title: "Not a repeat delivery",
    sub: "per service · 24 hours · 1,024 entries",
    out: "skip",
    outSub: "that service only",
  },
  {
    title: "Deployment enqueued",
    sub: "trigger: push",
    out: "202",
    outSub: "or 503 so GitHub redelivers",
    bad: false,
  },
];

const pitch = 54;

/** WebhookFlow is the ordered checks a push delivery goes through. */
export function WebhookFlow() {
  return (
    <Diagram
      width={680}
      height={pitch * gates.length + 6}
      label="Checks a webhook delivery passes in order, and the response each failing check returns"
    >
      {gates.map((g, i) => {
        const y = 6 + i * pitch;
        return (
          <g key={g.title}>
            <Node
              x={10}
              y={y}
              w={310}
              h={42}
              title={g.title}
              sub={g.sub}
              tone={i === gates.length - 1 ? "accent" : "neutral"}
              emphasis={i === gates.length - 1}
            />
            <Node
              x={420}
              y={y}
              w={250}
              h={42}
              title={g.out}
              sub={g.outSub}
              tone={g.bad ? "tomato" : "neutral"}
            />
            <Edge
              points={[
                [320, y + 21],
                [420, y + 21],
              ]}
              dashed
              tone={g.bad ? "tomato" : "neutral"}
              label={i === 6 ? "duplicate" : i < 4 ? "no" : undefined}
            />
            {i < gates.length - 1 && (
              <Edge
                points={[
                  [165, y + 42],
                  [165, y + pitch],
                ]}
                tone="accent"
              />
            )}
          </g>
        );
      })}
    </Diagram>
  );
}
