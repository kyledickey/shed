import { Link } from "@tanstack/react-router";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { CodeBlock, Doc, DocSection, DocTable, Figure, Term } from "../kit";
import { DepExplorer } from "./codebase/DepExplorer";

export function CodebaseDoc() {
  return (
    <Doc
      slug="codebase"
      lede="shed is one Go module with a React dashboard inside it. Each package under internal/ is a self-contained piece, and cmd/shed is the only place that knows how they fit together."
    >
      <DocSection page="codebase" id="layout">
        <CodeBlock title="repository">{`
cmd/shed/          entry point: flags, config, logging, wiring, shutdown
internal/
  config/          koanf → Config
  store/           SQLite, embedded migrations, CRUD on plain structs
  docker/          Docker Engine wrapper (moby client)
  build/           clone a commit, build an image (Dockerfile or Railpack)
  proxy/           embedded Caddy, Apply(routes)
  github/          GitHub App: manifest, tokens, repos, CI status, OAuth, webhooks
  vars/            \${{ KEY }} / \${{ service.KEY }} reference resolution
  catalog/         database templates
  deploy/          deployment pipeline, per-service workers, reconcile on boot
  host/            host resource usage from procfs, sysfs, and statfs
  logtail/         in-memory tail of shed's own log
  metrics/         container and host resource sampling, time series
  s3/              S3-compatible object storage client
  backup/          backups and restores of service data and shed.db
  auth/            sessions, GitHub sign-in, middleware
  api/             HTTP API, SSE logs, webhook, SPA serving
web/               dashboard (Vite+, React, TanStack Router + Query, CSS modules)
  embed.go         embeds web/dist into the Go binary
deploy/            systemd unit and example config
docs/design.md     data model, deploy pipeline, HTTP API contract
`}</CodeBlock>
        <p>
          <Term>docs/design.md</Term> is the source of truth for the data model, the deploy
          pipeline, and the HTTP API contract between Go and the dashboard. When you change any of
          those, update it in the same change. <Term>web/src/api/types.ts</Term> mirrors the types
          in it, so those two files change together too.
        </p>
      </DocSection>

      <DocSection page="codebase" id="packages">
        <p>
          The last column is the real import graph, taken from <Term>go list</Term>. Leaf packages
          import nothing from <Term>internal/</Term>.
        </p>
        <DocTable
          mono
          head={["Package", "Responsibility", "Imports from internal/"]}
          rows={[
            ["cmd/shed", "Flags, config, logging, wiring, signals, shutdown.", "everything"],
            [
              "config",
              "Loads TOML and SHED_* env into a Config with defaults and validation.",
              "none",
            ],
            ["store", "SQLite (modernc), embedded migrations, CRUD on plain structs.", "none"],
            ["docker", "Docker Engine operations through the moby client.", "none"],
            [
              "build",
              "Clones a commit and builds an image from a Dockerfile or with Railpack.",
              "none",
            ],
            ["proxy", "Embedded Caddy. Apply(routes) swaps the active route table.", "none"],
            [
              "github",
              "GitHub App: manifest, JWT, installation tokens, repos, CI status, OAuth, webhooks.",
              "none",
            ],
            ["vars", "Resolves ${{ KEY }} and ${{ service.KEY }} references.", "none"],
            ["catalog", "Database templates: image, port, volume path, default variables.", "none"],
            ["host", "Host CPU, memory, network, disk I/O, and filesystem usage.", "none"],
            ["logtail", "In-memory tail of shed's own log, followed over SSE.", "none"],
            ["s3", "S3-compatible object storage client for off-site backups.", "none"],
            [
              "deploy",
              "Deployment pipeline, per-service workers, reconcile on boot.",
              "build, catalog, docker, github, proxy, store, vars",
            ],
            [
              "metrics",
              "Container and host sampling, per-service and host time series.",
              "docker, host, store",
            ],
            [
              "backup",
              "Dumps, archives, zstd, age, schedule, retention, upload, restore.",
              "docker, store",
            ],
            ["auth", "Sessions, GitHub sign-in handlers, middleware.", "github"],
            [
              "api",
              "JSON API, SSE logs, webhook endpoint, SPA serving.",
              "auth, backup, catalog, deploy, github, logtail, metrics, store",
            ],
          ]}
        />
        <p>
          <Term>web</Term> is a Go package too, but only to embed the built dashboard. Its{" "}
          <Term>Dist()</Term> returns the files as an <Term>fs.FS</Term>.
        </p>
        <Figure caption="Arrows point from a package to what it imports. cmd/shed imports all of them and is not drawn with arrows.">
          <DependencyDiagram />
        </Figure>
        <DepExplorer />
        <p>
          Most imports from a consumer are for plain types, such as <Term>store.Service</Term>. The
          behavior a consumer needs from another package goes through an interface it declares
          itself, as the next sections describe.
        </p>
      </DocSection>

      <DocSection page="codebase" id="principles">
        <ul>
          <li>
            <strong>Leaf packages import nothing from internal/.</strong> That is what lets you
            understand, test, and replace one on its own. Keep it that way when you add code.
          </li>
          <li>
            <strong>Interfaces belong to the consumer.</strong> <Term>deploy.Docker</Term>,{" "}
            <Term>backup.Remote</Term>, <Term>auth.Store</Term>, and <Term>api.Deployer</Term> are
            small and name only what that package calls. Tests pass fakes.
          </li>
          <li>
            <strong>Dependencies are passed in.</strong> No package-level state, no{" "}
            <Term>init</Term> side effects, no global loggers. Each constructor takes a{" "}
            <Term>Config</Term> struct and a <Term>*slog.Logger</Term>.
          </li>
          <li>
            <strong>Caddy is the one exception.</strong> Caddy keeps process-global state, so{" "}
            <Term>internal/proxy</Term> contains it, and nothing else touches Caddy.
          </li>
          <li>
            <strong>APIs stay narrow.</strong> Export only what callers use. Prefer plain structs
            and functions over frameworks.
          </li>
          <li>
            <strong>New code goes where it belongs.</strong> Put it in the package that owns the
            concern, or in a new leaf package if it is separable. There is no <Term>util</Term>{" "}
            package.
          </li>
        </ul>
      </DocSection>

      <DocSection page="codebase" id="wiring">
        <p>
          <Term>cmd/shed/main.go</Term> builds every piece in dependency order and hands each one
          the real implementations of the interfaces it declared. The order is: config, logger, data
          directories, store, Docker client, proxy, deployer, backups, metrics collector, auth, API
          server. Then it recovers interrupted restores, reconciles services, starts the background
          loops, and serves HTTP until the process gets a signal.
        </p>
        <p>
          Most real types satisfy the consumer's interface directly: the one{" "}
          <Term>*docker.Client</Term> is passed to <Term>deploy</Term>, <Term>backup</Term>, and{" "}
          <Term>metrics</Term>, each of which sees only its own small interface. Where the shapes
          differ, <Term>main.go</Term> uses a small adapter:
        </p>
        <ul>
          <li>
            <Term>backupServices</Term> adapts the <Term>*deploy.Deployer</Term> to{" "}
            <Term>backup.Services</Term>, which lets a backup hold a service while it dumps data.
          </li>
          <li>
            <Term>api.AuthStore(st)</Term> adapts the store to <Term>auth.Store</Term>.
          </li>
          <li>
            A <Term>NewRemote</Term> closure converts <Term>backup.S3Config</Term> to{" "}
            <Term>s3.Config</Term>, so <Term>backup</Term> never imports <Term>s3</Term>.
          </li>
          <li>
            A <Term>GitHub</Term> closure returns the client from <Term>api.GitHubHolder</Term>, or
            false before the GitHub App is set up. <Term>deploy</Term> and <Term>auth</Term> call it
            each time instead of holding a client.
          </li>
          <li>
            <Term>deploy.Proxy</Term> is left nil when <Term>proxy.enabled</Term> is false, which
            turns routing off.
          </li>
        </ul>
        <CodeBlock title="cmd/shed/main.go (abridged)" lang="go">{`
var routes deploy.Proxy // nil disables routing
if cfg.Proxy.Enabled {
    routes = proxy.New(proxy.Config{ /* ports, ACME email, storage */ })
}

deployer := deploy.New(deploy.Config{
    Store: st, Docker: dc, Builder: &build.Builder{ /* ... */ },
    Proxy: routes, GitHub: githubFromHolder, Log: log,
})
backups := backup.New(backup.Config{
    Store: st, Docker: dc, Services: backupServices{deployer},
    NewRemote: func(c backup.S3Config) (backup.Remote, error) {
        return s3.New(s3.Config(c))
    },
})
`}</CodeBlock>
        <p>
          To add a new capability, write it as a package with its own interfaces, then add the
          wiring here. If it needs to be visible in the dashboard, add the endpoint to{" "}
          <Term>internal/api</Term> and describe it in <Term>docs/design.md</Term>. For how the
          larger pieces work inside, read{" "}
          <Link to="/docs/$slug" params={{ slug: "internals" }}>
            Internals
          </Link>
          .
        </p>
      </DocSection>
    </Doc>
  );
}

const consumers = [
  { x: 40, title: "deploy", sub: "7 leaf packages" },
  { x: 230, title: "metrics", sub: "docker host store" },
  { x: 420, title: "backup", sub: "docker store" },
  { x: 610, title: "auth", sub: "github" },
];

const leaves = [
  "store",
  "docker",
  "build",
  "proxy",
  "github",
  "vars",
  "catalog",
  "host",
  "s3",
  "config",
  "logtail",
];

function DependencyDiagram() {
  return (
    <Diagram width={800} height={440} label="Which internal packages import which">
      <Node
        x={10}
        y={8}
        w={780}
        h={44}
        title="cmd/shed"
        sub="builds each piece and injects the real implementations"
        tone="accent"
        emphasis
      />
      <Node x={320} y={88} w={160} h={44} title="api" sub="HTTP · SSE · webhook" />
      <Edge
        points={[
          [400, 52],
          [400, 88],
        ]}
        flow
        tone="accent"
      />
      {consumers.map((c) => (
        <g key={c.title}>
          <Node x={c.x} y={176} w={150} h={44} title={c.title} sub={c.sub} />
          <Edge
            points={[
              [400, 132],
              [400, 154],
              [c.x + 75, 154],
              [c.x + 75, 176],
            ]}
          />
          <Edge
            points={[
              [c.x + 75, 220],
              [c.x + 75, 250],
            ]}
          />
        </g>
      ))}
      <Zone
        x={10}
        y={250}
        w={780}
        h={150}
        label="leaf packages · import nothing from internal/"
        tone="grass"
      />
      {leaves.map((name, i) => {
        const row = i < 6 ? 0 : 1;
        const col = i < 6 ? i : i - 6;
        return (
          <Node key={name} x={24 + col * 126} y={282 + row * 54} w={110} h={40} title={name} />
        );
      })}
      <Note x={400} y={424} anchor="middle">
        api also imports store, github, logtail, and catalog directly.
      </Note>
    </Diagram>
  );
}
