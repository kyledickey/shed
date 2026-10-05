import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** ArchitectureDiagram shows shed's moving parts inside and around the one process. */
export function ArchitectureDiagram() {
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

/** RequestDiagram shows how the Host header picks the dashboard or a container. */
export function RequestDiagram() {
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
