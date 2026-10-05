import { Link } from "@tanstack/react-router";
import { Doc, DocSection, DocTable, Figure, Term } from "../kit";
import { ModelDiagram } from "./concepts/ModelDiagram";
import { StatusDiagram } from "./concepts/StatusDiagram";
import { WhatIf } from "./concepts/WhatIf";

export function ConceptsDoc() {
  return (
    <Doc
      slug="concepts"
      lede="A project is a group of services that run together on one private network. A service is an app or a database. There are no environments: for staging, make a second project."
    >
      <DocSection page="concepts" id="projects">
        <p>
          A project is a name and a Docker network. Its name is unique across the whole instance,
          and every service in it joins the network <code>shed-&lt;projectID&gt;</code>. Services in
          the same project reach each other by service name; services in different projects can't
          see each other.
        </p>
        <p>
          Deleting a project tears down everything under it: containers, volumes, images, build
          logs, and finally the network.
        </p>
        <Figure caption="The data model. Solid arrows are ownership; dashed arrows are what shed creates from it.">
          <ModelDiagram />
        </Figure>
      </DocSection>

      <DocSection page="concepts" id="services">
        <p>
          A service is one workload. Its <strong>name</strong> must be a DNS label (lowercase
          letters, digits, and hyphens, at most 63 characters), is unique within its project, and
          can't be changed after creation, because it is also the service's private hostname.
          Creating a service starts its first deployment immediately, with the trigger{" "}
          <Term>create</Term>.
        </p>
        <DocTable
          mono
          head={["Setting", "Meaning and default"]}
          rows={[
            [
              "repo, branch",
              "An app built from a GitHub repository. Pushes to the branch deploy it.",
            ],
            [
              "rootDir",
              "Subdirectory of the repo used as the build context. Empty means the root.",
            ],
            ["dockerfilePath", "Dockerfile relative to rootDir. Empty means auto-detect."],
            ["image", "An app run from a Docker image instead, or a database's image."],
            ["port", "Container port. New repo apps start at 8080 and receive it as PORT."],
            ["startCommand", "Overrides the image's command."],
            ["healthcheckPath", "Must start with /. Empty means a TCP connect to the port."],
            ["publicPort", "Publishes a host TCP port. Needs a container port."],
            [
              "cpuLimit, memoryLimit",
              "1 core and 1 GiB by default. 0 is unlimited; memory must be at least 64 MiB.",
            ],
            ["autoDeploy", "On by default. Deploy on every push to the branch."],
            ["waitForCi", "Off by default. Hold each deployment until the commit's CI passes."],
          ]}
        />
        <p>
          Reaching a service from another one is a matter of its name and port, such as{" "}
          <code>postgres:5432</code>. The{" "}
          <Link to="/docs/$slug" params={{ slug: "networking" }} hash="private-network">
            private network
          </Link>{" "}
          page covers how that name moves between containers during a deploy.
        </p>
      </DocSection>

      <DocSection page="concepts" id="service-kinds">
        <p>
          There are five kinds. An <Term>app</Term> is yours: shed builds it from a repository or
          pulls an image. The four databases come from built-in templates, which fix the image,
          port, and data directory, and generate the variables an app needs to connect.
        </p>
        <DocTable
          mono
          head={["Kind", "Image", "Port", "Volume at", "Variables created"]}
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
          Passwords are 24 random alphanumeric characters, so they are safe to embed in URLs. Redis
          starts with <code>--requirepass</code> and <code>--appendonly yes</code>. The Postgres
          volume covers <code>/var/lib/postgresql</code>, not the data directory itself, because
          Postgres 18 images keep data in a version-specific subdirectory.
        </p>
      </DocSection>

      <DocSection page="concepts" id="service-status">
        <p>
          A service's status isn't stored. Every time you ask, shed derives it from the service's
          newest deployment, its stopped flag, and whether the active deployment's container is
          running in Docker. The rules are checked top to bottom and the first match wins.
        </p>
        <Figure caption="The first rule whose condition holds decides the status.">
          <StatusDiagram />
        </Figure>
        <p>
          Two consequences are worth knowing. A failed redeploy doesn't change the status of a
          service that still has an active deployment, because the old container keeps serving: it
          stays <Term>active</Term>. And <Term>crashed</Term> is detected by the container's
          absence, not by an event, so it can appear on any listing after Docker reports the
          container isn't running.
        </p>
        <WhatIf />
        <p>
          A restore fence doesn't change status. It blocks deploys, starts, and restarts of the
          service until its restore finishes; see{" "}
          <Link to="/docs/$slug" params={{ slug: "restores" }} hash="fences">
            restore fences
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="concepts" id="settings-apply">
        <p>
          Settings are saved immediately, but a running container was created from the settings that
          existed when it started. Most settings reach it with the next deployment. A few take
          effect right away because shed reads them at the moment they matter.
        </p>
        <DocTable
          head={["Setting", "Takes effect"]}
          rows={[
            ["Domains", "Immediately. shed reloads the proxy routes when you add or remove one."],
            [
              "autoDeploy, branch, repo",
              "Immediately for the next push: the webhook reads them from the database.",
            ],
            ["waitForCi", "At the start of the next deployment."],
            [
              "Variables",
              "Next deployment. A container's environment is fixed when it is created.",
            ],
            [
              "port, publicPort, startCommand, healthcheckPath",
              "Next deployment. Until then routes and a recreated container keep the active deployment's recorded port.",
            ],
            ["cpuLimit, memoryLimit", "Next deployment, when the container is created."],
            ["Volumes", "Next deployment. A new mount is added to the next container."],
            ["rootDir, dockerfilePath, image", "Next build or pull."],
          ]}
        />
        <p>
          Restart doesn't re-read settings, since it restarts the existing container. Start
          recreates the container only if it has gone missing, and then uses the current variables
          with the recorded port. To apply a change, deploy; see{" "}
          <Link to="/docs/$slug" params={{ slug: "deployments" }} hash="pipeline">
            the pipeline
          </Link>
          .
        </p>
      </DocSection>
    </Doc>
  );
}
