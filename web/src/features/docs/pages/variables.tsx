import { Link } from "@tanstack/react-router";
import { Callout } from "../../../components/Misc";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { CodeBlock, Doc, DocSection, DocTable, Figure } from "../kit";
import { MAX_REFERENCE_DEPTH, MAX_RESOLVED_BYTES, MAX_VALUE_BYTES } from "../lib/vars";
import { Playground } from "./variables/Playground";

const ref = (body: string) => `\${{ ${body} }}`;
const tight = (body: string) => `\${{${body}}}`;

export function VariablesDoc() {
  return (
    <Doc
      slug="variables"
      lede="A variable is a key and a string. Values can point at other variables, in the same service or in a sibling, and shed expands those references into plain environment variables when a deployment starts."
    >
      <DocSection page="variables" id="references">
        <p>
          Write a reference inside any value and shed replaces it with the value it points to. This
          is how an app gets its database URL without anyone copying a password around.
        </p>
        <DocTable
          head={["You write", "It expands to"]}
          rows={[
            [
              <code key="a">{ref("KEY")}</code>,
              "Variable KEY of the same service, stored or injected.",
            ],
            [
              <code key="b">{ref("db.KEY")}</code>,
              "Variable KEY of the service named db in the same project. The reference is split at the first dot, so db.a.b means service db, key a.b.",
            ],
            [
              <code key="c">{"${{KEY}}"}</code>,
              "The same. Whitespace inside the braces is optional.",
            ],
            [
              <code key="d">{ref("NOPE")}</code>,
              "An empty string. A missing variable or service is not an error.",
            ],
            [
              <code key="e">{"${{ a b }}  ${{}}  ${A}  $HOME"}</code>,
              "Left exactly as written. Only ${{ name }} with no spaces in the name is a reference.",
            ],
          ]}
        />
        <p>
          A reference is expanded in the scope of the service that owns the value. When{" "}
          <code>web</code> uses <code>{ref("postgres.DATABASE_URL")}</code>, the{" "}
          <code>{ref("POSTGRES_USER")}</code> inside it means postgres's user, not web's, even if
          web has a variable with the same name.
        </p>
        <Figure caption="web asks for postgres.DATABASE_URL. Everything inside that value is looked up in postgres.">
          <ReferenceDiagram />
        </Figure>
        <CodeBlock title="on the web service" lang="env">
          {`DATABASE_URL=${ref("postgres.DATABASE_URL")}
REDIS_URL=${ref("redis.REDIS_URL")}
PUBLIC_URL=https://${ref("SHED_PUBLIC_DOMAIN")}`}
        </CodeBlock>
        <p>
          References resolve when a deployment starts, and the result is written into the new
          container's environment. Editing a variable does not change containers that are already
          running; deploy again to apply it. Builds get the resolved variables too, see{" "}
          <Link to="/docs/$slug" params={{ slug: "builds" }} hash="build-secrets">
            build secrets
          </Link>
          . Saving only checks key names (<code>[A-Za-z_][A-Za-z0-9_]*</code>), so a bad reference
          shows up as a failed deployment, not as a save error.
        </p>
      </DocSection>

      <DocSection page="variables" id="injected">
        <p>
          shed adds a few variables to every service. You can reference them like any other, from
          the same service or another one (<code>{ref("postgres.SHED_PRIVATE_DOMAIN")}</code>).
        </p>
        <DocTable
          mono
          head={["Variable", "Value", "Present when"]}
          rows={[
            ["PORT", "the service's port", "an app with a port above 0"],
            ["SHED_PROJECT_NAME", "the project's name", "always"],
            ["SHED_SERVICE_NAME", "the service's name", "always"],
            ["SHED_PRIVATE_DOMAIN", "the service's name, its private host", "always"],
            ["SHED_PUBLIC_DOMAIN", "the host of the first domain", "the service has a domain"],
            [
              "SHED_GIT_COMMIT_SHA",
              "the commit being deployed",
              "a commit is known, and only for the service being deployed",
            ],
            ["SHED_GIT_BRANCH", "the service's branch", "the service has a branch"],
          ]}
        />
        <p>
          Your own variables win. If you store a variable with the same name as an injected one,
          yours replaces it. The merged set is then resolved, and the container receives it sorted
          by key.
        </p>
        <Figure caption="How one service's environment is assembled at deploy time.">
          <MergeDiagram />
        </Figure>
        <Callout tone="sky" title="Commits and ports are per deployment">
          <code>SHED_GIT_COMMIT_SHA</code> is only set on the service being deployed. A reference
          like <code>{ref("web.SHED_GIT_COMMIT_SHA")}</code> from another service expands to
          nothing. A service with no port gets the lowest port its image exposes after the build,
          and shed resolves its variables again so <code>PORT</code> appears.
        </Callout>
      </DocSection>

      <DocSection page="variables" id="templates">
        <p>
          Creating a database writes its variables for you. Passwords are 24 random characters from{" "}
          <code>A-Za-z0-9</code>, drawn uniformly at random, so they are safe to embed in a URL
          without escaping. Every database also gets a volume at the template's data path, see{" "}
          <Link to="/docs/$slug" params={{ slug: "resources" }} hash="volumes">
            volumes
          </Link>
          .
        </p>
        <DocTable
          mono
          head={["Kind", "Image", "Port", "Volume", "Variables created"]}
          rows={[
            [
              "postgres",
              "postgres:18-alpine",
              "5432",
              "/var/lib/postgresql",
              "POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB, DATABASE_URL",
            ],
            [
              "mysql",
              "mysql:9",
              "3306",
              "/var/lib/mysql",
              "MYSQL_ROOT_PASSWORD, MYSQL_DATABASE, MYSQL_URL, DATABASE_URL",
            ],
            [
              "mongo",
              "mongo:8",
              "27017",
              "/data/db",
              "MONGO_INITDB_ROOT_USERNAME, MONGO_INITDB_ROOT_PASSWORD, MONGO_URL",
            ],
            ["redis", "redis:8-alpine", "6379", "/data", "REDIS_PASSWORD, REDIS_URL"],
          ]}
        />
        <p>
          The connection strings are themselves references, so they stay correct if you rename the
          user or the database. This is what postgres starts with:
        </p>
        <CodeBlock title="postgres service" lang="env">
          {`POSTGRES_USER=postgres
POSTGRES_PASSWORD=<24 random characters>
POSTGRES_DB=app
DATABASE_URL=postgresql://${tight("POSTGRES_USER")}:${tight("POSTGRES_PASSWORD")}@${tight("SHED_PRIVATE_DOMAIN")}:5432/${tight("POSTGRES_DB")}`}
        </CodeBlock>
        <p>
          Two details matter in practice. Redis runs <code>redis-server --requirepass</code> with{" "}
          <code>$REDIS_PASSWORD</code> and <code>--appendonly yes</code>, so changing the password
          variable takes effect on the next deploy. Postgres, MySQL, and MongoDB only read their
          password variables when the data directory is first initialized, so changing them later
          does not change the password inside an existing database.
        </p>
      </DocSection>

      <DocSection page="variables" id="playground">
        <p>
          This runs the resolver on a sample project with a web app, postgres, and redis. Pick a
          service and it merges the injected variables, applies the stored ones, and expands every
          reference the way a deployment does. Click a reference to jump to its source. Edit any
          value to watch the result change, or add a scratch variable to cause a cycle.
        </p>
        <Playground />
      </DocSection>

      <DocSection page="variables" id="resolution">
        <p>
          shed expands each of the service's variables depth first, remembering finished values and
          tracking which variables it is currently inside of. The rules, in order:
        </p>
        <ol>
          <li>
            Keys are visited in sorted order, so the first cycle reported is always the same one.
          </li>
          <li>
            A variable that is missing resolves to an empty string before any other check, even if
            it would be on the stack.
          </li>
          <li>A variable already on the stack is a cycle. The error lists the loop:</li>
        </ol>
        <CodeBlock lang="error">{`vars: reference cycle: web.A -> web.B -> web.A`}</CodeBlock>
        <ol start={4}>
          <li>
            Reaching a variable deeper than {MAX_REFERENCE_DEPTH} levels, or one over the size
            limits, is an error.
          </li>
          <li>
            Finished values are memoized, so a diamond (<code>A</code> uses <code>B</code> and{" "}
            <code>C</code>, both use <code>D</code>) is not a cycle, and two services may refer to
            each other as long as the variables do not.
          </li>
        </ol>
        <p>
          The first error fails the whole set, and therefore the deployment, with{" "}
          <code>deploy: resolve variables: ...</code> in the build log. Only the variables of the
          service being deployed are walked: another service's broken value does not matter unless
          something you deploy references it.
        </p>
        <p>
          After resolution, the values of your <em>stored</em> variables become literal secrets that
          shed masks in build and runtime logs. Injected values like <code>PORT</code> are not
          masked. See{" "}
          <Link to="/docs/$slug" params={{ slug: "logs" }} hash="redaction">
            secret redaction
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="variables" id="limits">
        <p>
          Limits are checked as values are expanded, before the bytes are appended, so a hostile
          chain cannot exhaust memory. All sizes are UTF-8 bytes.
        </p>
        <DocTable
          head={["Limit", "Value", "Error"]}
          rows={[
            [
              "One value, as stored",
              `${MAX_VALUE_BYTES / 1024} KiB`,
              <code key="a">vars: value web.A exceeds 65536 bytes</code>,
            ],
            [
              "One value, after expansion",
              `${MAX_VALUE_BYTES / 1024} KiB`,
              <code key="b">vars: expanded value exceeds 65536 bytes</code>,
            ],
            [
              "All resolved values together",
              `${MAX_RESOLVED_BYTES / (1 << 20)} MiB`,
              <code key="c">vars: resolved values exceed 1048576 bytes</code>,
            ],
            [
              "Reference chain depth",
              String(MAX_REFERENCE_DEPTH),
              <code key="d">vars: reference depth exceeds 64 at web.V064</code>,
            ],
            ["Variable name", "[A-Za-z_][A-Za-z0-9_]*", "400 when saving"],
          ]}
        />
        <p>
          The playground shows the resolved size against the 1 MiB budget. The total counts each
          variable once, including ones reached through another service. The{" "}
          <Link to="/docs/$slug" params={{ slug: "internals" }} hash="resolver">
            resolver internals
          </Link>{" "}
          cover the algorithm in code.
        </p>
      </DocSection>
    </Doc>
  );
}

function ReferenceDiagram() {
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

function MergeDiagram() {
  return (
    <Diagram
      width={800}
      height={96}
      label="Injected variables, stored variables, resolution, container environment"
    >
      <Node x={0} y={20} w={170} title="Injected by shed" sub="SHED_*, PORT" tone="accent" />
      <Node x={210} y={20} w={170} title="Your variables" sub="win on the same key" />
      <Node x={420} y={20} w={170} title="Resolve references" sub="per owning service" emphasis />
      <Node x={630} y={20} w={170} title="Container env" sub="KEY=value, sorted" tone="grape" />
      {[170, 380, 590].map((x) => (
        <Edge
          key={x}
          points={[
            [x, 46],
            [x + 40, 46],
          ]}
        />
      ))}
    </Diagram>
  );
}
