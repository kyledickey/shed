import { Link } from "@tanstack/react-router";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { CodeBlock, Doc, DocSection, DocTable, Figure, Term } from "../kit";

export function LocalDevelopmentDoc() {
  return (
    <Doc
      slug="local-development"
      lede="Run the Go server and the dashboard dev server side by side on your machine, with the proxy off and a throwaway data directory."
    >
      <DocSection page="local-development" id="prerequisites">
        <DocTable
          head={["Tool", "Why"]}
          rows={[
            [
              <Term key="go">Go 1.27</Term>,
              "The version in go.mod. Builds the server and runs Go tests.",
            ],
            [
              <Term key="docker">Docker with buildx</Term>,
              "shed runs every workload and builds every image through it, and the server pings the daemon at startup.",
            ],
            [<Term key="git">git</Term>, "Clones the commit that a build uses."],
            [
              <Term key="railpack">railpack</Term>,
              "Builds apps that have no Dockerfile. It must be on PATH.",
            ],
            [<Term key="bun">bun</Term>, "Installs dashboard dependencies and runs Vite+."],
          ]}
        />
        <p>
          shed runs on Linux only. You also need a GitHub account for sign-in and repository access.
          shed creates its GitHub App for you.
        </p>
      </DocSection>

      <DocSection page="local-development" id="running">
        <p>
          Two processes run side by side. Vite serves the dashboard with hot reload and proxies{" "}
          <Term>/api</Term> to the Go server.
        </p>
        <Figure caption="Open the dashboard at the Vite address. The Go server only sees API calls.">
          <DevDiagram />
        </Figure>
        <h3>1. Configure</h3>
        <p>
          Create <Term>shed.dev.toml</Term> in the repository root. It is gitignored, and{" "}
          <Term>make dev</Term> reads it.
        </p>
        <CodeBlock title="shed.dev.toml">{`
[server]
listen = "127.0.0.1:3000"
url = "http://localhost:5173"

[data]
dir = ".dev/data"

[proxy]
enabled = false
base_domain = "localhost"

[log]
level = "debug"

[auth]
allowed_users = ["your-github-login"]
`}</CodeBlock>
        <ul>
          <li>
            <Term>server.url</Term> must be the exact origin you open the dashboard at. The API
            rejects any write whose <Term>Origin</Term> header differs from it with{" "}
            <Term>untrusted request origin</Term>. It is also the base of GitHub callbacks and
            webhooks. If you open the dashboard through a tunnel or a dev domain so that GitHub can
            reach your machine, use that origin here instead.
          </li>
          <li>
            <Term>proxy.enabled = false</Term> stops shed from starting Caddy on ports 80 and 443.
            Services still deploy, but nothing routes to them.
          </li>
          <li>
            <Term>data.dir</Term> holds <Term>shed.db</Term>, builds, logs, and backups.{" "}
            <Term>.dev/</Term> is gitignored. Delete it to start from scratch.
          </li>
          <li>
            <Term>auth.allowed_users</Term> lists the GitHub logins that may sign in. Set it before
            your first sign-in.
          </li>
        </ul>
        <p>
          Every key also accepts a <Term>SHED_*</Term> environment variable, such as{" "}
          <Term>SHED_SERVER_URL</Term>. See{" "}
          <Link to="/docs/$slug" params={{ slug: "configuration" }}>
            Configuration
          </Link>
          .
        </p>

        <h3>2. Start the server</h3>
        <CodeBlock lang="sh">{`
make dev            # go run ./cmd/shed -config shed.dev.toml
# or build the full binary, dashboard included
make build
bin/shed -config shed.dev.toml
`}</CodeBlock>
        <p>
          The log goes to stderr and to <Term>shed.log</Term> next to the config file, so in dev it
          is in the repository root. On the first run, with no GitHub App yet, shed logs a warning
          with the setup page URL and a one-time <Term>token</Term>. Open the URL and enter the
          token to create the App. See{" "}
          <Link to="/docs/$slug" params={{ slug: "github" }} hash="setup">
            First-run setup
          </Link>
          .
        </p>

        <h3>3. Start the dashboard</h3>
        <CodeBlock lang="sh">{`
cd web
bun install        # first time
bun run dev        # Vite+ dev server, proxies /api to 127.0.0.1:3000
`}</CodeBlock>
        <p>
          Open <Term>http://localhost:5173</Term> (or whatever origin you put in{" "}
          <Term>server.url</Term>). Edits to the dashboard hot-reload.
        </p>

        <h3>How the dashboard reaches the binary</h3>
        <p>
          <Term>make build</Term> runs <Term>bun run build</Term>, which writes{" "}
          <Term>web/dist</Term>. <Term>web/embed.go</Term> embeds that directory with{" "}
          <Term>{"//go:embed all:dist"}</Term>, and the API serves it as a single-page app. The
          embed happens at compile time, so after you change the dashboard you must run{" "}
          <Term>make build</Term> again before <Term>bin/shed</Term> shows it. With{" "}
          <Term>bun run dev</Term> you never need to, because Vite serves the source directly.
        </p>
        <p>
          <Term>web/dist</Term> is gitignored apart from a <Term>.gitkeep</Term>, which is why{" "}
          <Term>go run</Term> compiles on a fresh clone.
        </p>
      </DocSection>

      <DocSection page="local-development" id="testing">
        <CodeBlock lang="sh">{`
make test                       # go test -race ./...
go vet ./...
gofmt -l .                      # should print nothing

cd web
bunx vp check                   # format, lint, typecheck
bunx vp check --fix             # apply formatting and safe fixes
bunx vp test run                # dashboard unit tests
`}</CodeBlock>
        <p>
          Go code must pass <Term>gofmt</Term>, <Term>go vet</Term>, and <Term>go test -race</Term>.
          Unit tests need no Docker daemon and no network. Packages test against fakes of the
          interfaces they declare, so <Term>deploy</Term> tests run a whole pipeline against a fake
          Docker, builder, and store.
        </p>
        <h3>Integration tests</h3>
        <p>
          Tests that need real infrastructure carry the <Term>integration</Term> build tag, so{" "}
          <Term>make test</Term> skips them:
        </p>
        <CodeBlock lang="sh">{`
go test -race -tags integration ./internal/docker ./internal/backup ./internal/s3
`}</CodeBlock>
        <DocTable
          head={["Package", "Needs"]}
          rows={[
            [
              <Term key="d">internal/docker</Term>,
              "A Docker daemon. The tests skip when none responds.",
            ],
            [
              <Term key="b">internal/backup</Term>,
              "A Docker daemon, for real database dumps and restores. Skips when none responds.",
            ],
            [
              <Term key="s">internal/s3</Term>,
              <>
                An S3-compatible endpoint, given by <Term>SHED_S3_TEST_ENDPOINT</Term>,{" "}
                <Term>SHED_S3_TEST_BUCKET</Term>, <Term>SHED_S3_TEST_ACCESS_KEY</Term>, and{" "}
                <Term>SHED_S3_TEST_SECRET_KEY</Term>. Skips unless all four are set.
              </>,
            ],
          ]}
        />
        <p>
          Some unit tests also skip on their own when a tool is missing, such as <Term>git</Term>{" "}
          for the build tests.
        </p>
      </DocSection>

      <DocSection page="local-development" id="conventions">
        <ul>
          <li>
            <strong>Go style.</strong> Follow the Google Go style guide and Go doc comments. Every
            package has a package comment, and every exported identifier has a doc comment that
            starts with its name.
          </li>
          <li>
            <strong>Errors.</strong> Wrap with context, for example{" "}
            <Term>{'fmt.Errorf("build: clone %s: %w", repo, err)'}</Term>. Return errors instead of
            logging and continuing. Use sentinel errors such as <Term>store.ErrNotFound</Term> for
            conditions callers branch on.
          </li>
          <li>
            <strong>Context and logging.</strong> <Term>context.Context</Term> is the first
            parameter of anything that does I/O. Log with <Term>log/slog</Term> through a logger
            that was passed in.
          </li>
          <li>
            <strong>Tests.</strong> Write table-driven tests for logic and fakes for interfaces.
          </li>
          <li>
            <strong>Design.</strong> Change <Term>docs/design.md</Term> in the same commit when you
            change the data model, the pipeline, or the API. Change{" "}
            <Term>web/src/api/types.ts</Term> with it.
          </li>
          <li>
            <strong>Generated files.</strong> <Term>web/src/routeTree.gen.ts</Term> is produced by
            the router plugin when Vite runs. Commit it with the route change that caused it.
          </li>
          <li>
            <strong>Commits.</strong> Use short, standard messages with little or no body, such as{" "}
            <Term>fix(deploy): keep candidate address on promotion</Term>. Work on a branch, not{" "}
            <Term>main</Term>.
          </li>
        </ul>
        <p>
          The{" "}
          <Link to="/docs/$slug" params={{ slug: "codebase" }}>
            Codebase
          </Link>{" "}
          page explains the package rules, and{" "}
          <Link to="/docs/$slug" params={{ slug: "dashboard" }}>
            Dashboard
          </Link>{" "}
          covers the frontend.
        </p>
      </DocSection>
    </Doc>
  );
}

function DevDiagram() {
  return (
    <Diagram width={800} height={260} label="Local development setup">
      <Zone x={170} y={30} w={170} h={110} label="bun run dev" tone="sky" />
      <Zone x={380} y={30} w={410} h={220} label="make dev · one Go process" tone="accent" />

      <Node x={10} y={74} w={130} title="Browser" sub="origin = server.url" />
      <Node x={186} y={74} w={138} title="Vite+" sub=":5173 · HMR" tone="sky" />
      <Node x={400} y={74} w={150} title="API" sub="127.0.0.1:3000" emphasis tone="accent" />
      <Node x={400} y={170} w={150} title="shed.db" sub=".dev/data" />
      <Node x={600} y={74} w={170} title="Docker Engine" sub="workloads · builds" />
      <Node x={600} y={170} w={170} title="Proxy off" sub="proxy.enabled = false" dim />

      <Edge
        points={[
          [140, 100],
          [186, 100],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [324, 100],
          [400, 100],
        ]}
        label="/api"
        flow
        tone="accent"
      />
      <Edge
        points={[
          [475, 126],
          [475, 170],
        ]}
      />
      <Edge
        points={[
          [550, 100],
          [600, 100],
        ]}
      />
      <Note x={10} y={170}>
        Everything except /api is served by Vite from web/src.
      </Note>
      <Note x={10} y={188}>
        The proxy is off, so deployed apps are not routed.
      </Note>
    </Diagram>
  );
}
