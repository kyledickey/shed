import { Diagram, Edge, Node, Zone } from "../../diagram";

const B = 100;
const S = 400;
const G = 700;

/** SignInDiagram is the GitHub OAuth sequence, as arrows between three lanes. */
export function SignInDiagram() {
  const arrow = (n: number, y: number, from: number, to: number, label: string) => (
    <Edge
      points={[
        [from, y],
        [to, y],
      ]}
      label={`${n} ${label}`}
      tone="accent"
      flow
    />
  );
  return (
    <Diagram width={800} height={400} label="Sign-in sequence between browser, shed, and GitHub">
      <Zone x={10} y={0} w={180} h={392} label="your browser" />
      <Zone x={310} y={0} w={180} h={392} label="shed" tone="accent" />
      <Zone x={610} y={0} w={180} h={392} label="GitHub" />
      {arrow(1, 64, B, S, "login")}
      {arrow(2, 96, S, B, "302 + state")}
      {arrow(3, 128, B, G, "authorize")}
      {arrow(4, 160, G, B, "code + state")}
      {arrow(5, 192, B, S, "callback")}
      {arrow(6, 224, S, G, "exchange")}
      {arrow(7, 256, G, S, "user")}
      <Node
        x={320}
        y={282}
        w={160}
        h={44}
        title="allowed_users?"
        sub="case-insensitive"
        emphasis
        tone="accent"
      />
      {arrow(8, 360, S, B, "cookie, 302 /")}
    </Diagram>
  );
}

/** MiddlewareDiagram is the sequence of checks a session route runs through, outermost first. */
export function MiddlewareDiagram() {
  const nodes = [
    { x: 20, w: 140, title: "Security headers", sub: "every response" },
    { x: 184, w: 110, title: "Session", sub: "401 no session" },
    { x: 318, w: 148, title: "Origin check", sub: "403 bad origin" },
    { x: 490, w: 118, title: "JSON only", sub: "415 not JSON" },
    { x: 632, w: 96, title: "Handler", sub: "your request" },
  ];
  return (
    <Diagram width={760} height={100} label="Checks applied to authenticated API routes">
      {nodes.map((n, i) => (
        <Node
          key={n.title}
          x={n.x}
          y={24}
          w={n.w}
          title={n.title}
          sub={n.sub}
          tone={i === 4 ? "accent" : "neutral"}
          emphasis={i === 4}
        />
      ))}
      {nodes.slice(0, -1).map((n, i) => (
        <Edge
          key={n.title}
          points={[
            [n.x + n.w, 50],
            [nodes[i + 1]!.x, 50],
          ]}
          flow
          tone="accent"
        />
      ))}
    </Diagram>
  );
}

/** WebhookDiagram is the order of checks a webhook delivery goes through. */
export function WebhookDiagram() {
  return (
    <Diagram width={800} height={200} label="Checks applied to a webhook delivery, in order">
      <Node x={15} y={30} w={170} title="Signature shape" sub="sha256=<64 hex> · 401" />
      <Node x={215} y={30} w={170} title="Guard" sub="8 burst · 4 at once" tone="accent" emphasis />
      <Node x={415} y={30} w={170} title="Read body" sub="≤ 1 MiB · 10 s · 413" />
      <Node x={615} y={30} w={170} title="Verify HMAC" sub="401 on mismatch" />
      <Edge
        points={[
          [185, 56],
          [215, 56],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [385, 56],
          [415, 56],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [585, 56],
          [615, 56],
        ]}
        flow
        tone="accent"
      />
      <Node x={615} y={130} w={170} title="Event filter" sub="not push → 202" />
      <Node x={415} y={130} w={170} title="Dedup" sub="body hash + service" />
      <Node x={215} y={130} w={170} title="Deploy" sub="trigger push · 202" />
      <Edge
        points={[
          [700, 82],
          [700, 130],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [615, 156],
          [585, 156],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [415, 156],
          [385, 156],
        ]}
        flow
        tone="accent"
      />
    </Diagram>
  );
}
