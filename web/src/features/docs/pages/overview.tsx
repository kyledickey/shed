import { Link } from "@tanstack/react-router";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { Doc, DocSection, Figure } from "../kit";

export function OverviewDoc() {
  return (
    <Doc
      slug="overview"
      lede="shed is a self-hosted deployment platform for one Linux server. Push to GitHub, and shed builds your app, runs it in Docker next to its databases, puts it behind HTTPS, and backs up its data."
    >
      <DocSection page="overview" id="what-is-shed">
        <p>
          shed is inspired by Railway, scaled down to one VPS. You get projects, services, variables
          with references, push-to-deploy, zero-downtime switchovers, metrics, logs, and backups,
          all running on hardware you control.
        </p>
        <ul>
          <li>
            <strong>One binary.</strong> A single Go process serves this dashboard and the JSON API,
            runs the deploy pipeline, samples metrics, schedules backups, and embeds{" "}
            <a href="https://caddyserver.com" target="_blank" rel="noreferrer">
              Caddy
            </a>{" "}
            as its reverse proxy. There is no separate proxy or queue to run.
          </li>
          <li>
            <strong>Docker runs every workload.</strong> Apps, databases, build helpers, and backup
            helpers are all containers. shed talks to the Docker Engine API directly.
          </li>
          <li>
            <strong>State lives in SQLite.</strong> Projects, services, deployments, metrics, and
            settings are in one file, <code>shed.db</code>, which shed also backs up.
          </li>
          <li>
            <strong>One GitHub App</strong> handles sign-in, repository access, push webhooks, and
            CI status. shed creates it for you on first run.
          </li>
        </ul>
        <p>
          There are no environments: a project is a set of services that run together. If you want
          staging, make a second project.
        </p>
      </DocSection>

      <DocSection page="overview" id="architecture">
        <p>
          Everything inside the dashed box is one process. Caddy terminates TLS on ports 80 and 443
          and routes the dashboard hostname to the API on <code>127.0.0.1:3000</code>. Every other
          hostname goes straight to the IP of a service's active container on its project's Docker
          network.
        </p>
        <Figure caption="shed's moving parts. Everything in the green box runs inside the one shed process.">
          <ArchitectureDiagram />
        </Figure>
        <p>
          If you want to read or change the code, the{" "}
          <Link to="/docs/$slug" params={{ slug: "codebase" }}>
            Codebase
          </Link>{" "}
          page maps each of these parts to its Go package.
        </p>
      </DocSection>

      <DocSection page="overview" id="request-path">
        <p>
          A request takes one of two paths, chosen by its <code>Host</code> header. Requests for the
          dashboard host go to the API, which checks your session. Requests for any other domain go
          to a container and never touch the API.
        </p>
        <Figure caption="Top: you, using the dashboard. Bottom: a visitor using an app you deployed.">
          <RequestDiagram />
        </Figure>
        <p>
          Inside a project, services find each other by name. The active container of service{" "}
          <code>postgres</code> has the network alias <code>postgres</code> on the project network{" "}
          <code>shed-&lt;projectID&gt;</code>, so an app connects to <code>postgres:5432</code>.
          Nothing is published on the host unless you set a{" "}
          <Link to="/docs/$slug" params={{ slug: "networking" }} hash="public-port">
            public TCP port
          </Link>
          .
        </p>
      </DocSection>
    </Doc>
  );
}

function ArchitectureDiagram() {
  return (
    <Diagram width={800} height={450} label="Architecture of shed">
      <Zone x={160} y={44} w={390} h={396} label="shed · one Go process" tone="accent" />
      <Zone x={580} y={44} w={210} h={396} label="Docker Engine" />
      <Zone x={595} y={110} w={180} h={170} label="shed-<projectID>" tone="sky" />

      <Node x={10} y={84} w={120} title="Internet" sub="browsers, users" />
      <Node x={10} y={174} w={120} title="GitHub" sub="App API" />
      <Node x={10} y={354} w={120} title="S3 bucket" sub="off-site backups" />

      <Node x={180} y={84} w={150} title="Caddy" sub=":80 · :443" tone="accent" emphasis />
      <Node x={380} y={84} w={150} title="API + dashboard" sub="JSON API · sign-in" />
      <Node x={180} y={174} w={150} title="GitHub client" sub="App API · webhooks" />
      <Node x={380} y={174} w={150} title="SQLite" sub="shed.db" />
      <Node x={180} y={264} w={150} title="Metrics" sub="docker stats · procfs" />
      <Node x={380} y={264} w={150} title="Deploy pipeline" sub="one worker per service" />
      <Node x={180} y={354} w={150} title="Backups" sub="zstd · age · S3" />
      <Node x={380} y={354} w={150} title="Builds" sub="Dockerfile · Railpack" />

      <Node x={605} y={140} w={160} h={48} title="web" sub="app · :3000" tone="grape" />
      <Node x={605} y={215} w={160} h={48} title="postgres" sub="database · :5432" tone="sky" />
      <Node x={605} y={354} w={160} title="buildx builder" sub="--builder shed" />

      <Edge
        points={[
          [130, 110],
          [180, 110],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [330, 110],
          [380, 110],
        ]}
      />
      <Edge
        points={[
          [322, 84],
          [322, 24],
          [740, 24],
          [740, 140],
        ]}
        label="routes to container IP:port"
        flow
        tone="accent"
      />
      <Edge
        points={[
          [130, 200],
          [180, 200],
        ]}
        both
      />
      <Edge
        points={[
          [455, 136],
          [455, 174],
        ]}
      />
      <Edge
        points={[
          [455, 316],
          [455, 354],
        ]}
      />
      <Edge
        points={[
          [530, 290],
          [565, 290],
          [565, 164],
          [605, 164],
        ]}
      />
      <Edge
        points={[
          [685, 188],
          [685, 215],
        ]}
        dashed
      />
      <Edge
        points={[
          [530, 380],
          [605, 380],
        ]}
        label="build"
      />
      <Edge
        points={[
          [180, 380],
          [130, 380],
        ]}
        label="upload"
      />
      <Note x={556} y={302} anchor="middle" mono>
        create
      </Note>
    </Diagram>
  );
}

function RequestDiagram() {
  const lane = (y: number) => y + 26;
  const row = (y: number) => ({
    a: [150, lane(y)] as [number, number],
    b: [220, lane(y)] as [number, number],
    c: [370, lane(y)] as [number, number],
    d: [460, lane(y)] as [number, number],
    e: [610, lane(y)] as [number, number],
    f: [660, lane(y)] as [number, number],
  });
  const top = row(34);
  const bottom = row(164);
  return (
    <Diagram width={800} height={250} label="How requests reach the dashboard and apps">
      <Note x={10} y={22}>
        Host: shed.example.com
      </Note>
      <Node x={10} y={34} w={140} title="Your browser" sub="shed_session cookie" />
      <Node x={220} y={34} w={150} title="Caddy" sub="TLS · match host" tone="accent" emphasis />
      <Node x={460} y={34} w={150} title="API" sub="127.0.0.1:3000" />
      <Node x={660} y={34} w={130} title="SQLite, Docker" sub="shed.db" />
      <Edge points={[top.a, top.b]} flow tone="accent" />
      <Edge points={[top.c, top.d]} label="reverse_proxy" />
      <Edge points={[top.e, top.f]} />

      <Note x={10} y={152}>
        Host: app.example.com
      </Note>
      <Node x={10} y={164} w={140} title="Visitor" sub="any client" />
      <Node x={220} y={164} w={150} title="Caddy" sub="TLS · match host" tone="accent" emphasis />
      <Node x={460} y={164} w={150} title="web container" sub="172.18.0.4:3000" tone="grape" />
      <Node x={660} y={164} w={130} title="postgres" sub="postgres:5432" tone="sky" />
      <Edge points={[bottom.a, bottom.b]} flow tone="accent" />
      <Edge points={[bottom.c, bottom.d]} label="container IP" />
      <Edge points={[bottom.e, bottom.f]} label="alias" dashed />
      <Note x={460} y={238} mono>
        both on network shed-&lt;projectID&gt;
      </Note>
    </Diagram>
  );
}
