import { Link } from "@tanstack/react-router";
import { CodeBlock, Doc, DocSection, DocTable, Figure, Term } from "../kit";
import { BuilderPanel } from "./builds/BuilderPanel";
import { DetectionFlow } from "./builds/DetectionFlow";
import { DiskGuard } from "./builds/DiskGuard";
import { SecretSnippet } from "./builds/SecretSnippet";

export function BuildsDoc() {
  return (
    <Doc
      slug="builds"
      lede="shed clones your commit into a scratch directory and builds an image with docker buildx. It uses your Dockerfile when there is one and Railpack when there isn't. Builds run one at a time, on a resource-limited builder."
    >
      <DocSection page="builds" id="detection">
        <p>
          The build step of a{" "}
          <Link to="/docs/$slug" params={{ slug: "deployments" }} hash="pipeline">
            deployment
          </Link>{" "}
          does three things. It clones the exact commit with <code>git fetch --depth 1</code> into{" "}
          <code>&lt;data&gt;/builds/&lt;deploymentID&gt;/src</code>, using the GitHub App's
          installation token in a scoped HTTP header rather than on the command line. It picks a
          build tool. Then it builds, and deletes the workspace whatever the outcome.
        </p>
        <Figure caption="The choice happens after the clone, because shed has to look inside the repository.">
          <DetectionFlow />
        </Figure>
        <p>
          <Term>rootDir</Term> is the build context. A <Term>dockerfilePath</Term> is relative to
          it. Both must stay inside the repository: a path that escapes it, including through a
          symlink, fails the build. Setting a Dockerfile path is a promise, so a missing file is an
          error instead of a quiet fall-back to Railpack.
        </p>
      </DocSection>

      <DocSection page="builds" id="railpack">
        <p>
          <a href="https://railpack.com" target="_blank" rel="noreferrer">
            Railpack
          </a>{" "}
          inspects the source, works out the language and build steps, and writes a build plan. shed
          runs it in two phases, and the plan goes to a BuildKit frontend rather than a Dockerfile:
        </p>
        <CodeBlock lang="sh">
          {`# 1. Plan. Runs in the context directory; the plan and info files go to the
#    private workspace, outside the repository's source tree.
railpack prepare <contextDir> --plan-out <workspace>/railpack-plan.json \\
  --info-out <workspace>/railpack-info.json --env KEY --env KEY2 ...

# 2. Build the plan with Railpack's BuildKit frontend.
docker buildx build --builder shed --load -t shed/<serviceID>:<deploymentID> \\
  -f <workspace>/railpack-plan.json --secret id=KEY,src=<workspace>/secrets/KEY \\
  --build-arg BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend <contextDir>`}
        </CodeBlock>
        <p>
          Variable values reach <code>railpack prepare</code> through its process environment,
          because Railpack reads them while planning. Names that control host tools are withheld
          from that step: <code>PATH</code>, <code>HOME</code>, <code>TMPDIR</code>, and anything
          starting with <code>LD_</code>, <code>DYLD_</code>, <code>XDG_</code>, <code>GIT_</code>,
          or <code>DOCKER_</code>. They are still build secrets and still reach the running
          container.
        </p>
        <p>
          A Dockerfile build is the same second command with <code>-f &lt;your Dockerfile&gt;</code>{" "}
          and no frontend override.
        </p>
      </DocSection>

      <DocSection page="builds" id="builder">
        <p>
          shed doesn't build on Docker's default builder. It creates its own named <Term>shed</Term>
          , with the <code>docker-container</code> driver, which runs BuildKit in a container
          (buildx names it <code>buildx_buildkit_shed0</code>). That container is the only place
          builds consume CPU and memory, so a runaway build can't starve your running services.
        </p>
        <p>
          Container limits can only be set when a container is created. So the first build after
          shed starts runs <code>docker buildx rm --keep-state shed</code> and creates the builder
          again with the current <code>build.memory_mb</code> and <code>build.cpus</code>.{" "}
          <code>--keep-state</code> keeps the layer cache, so this costs nothing but a few seconds.
          Memory has no swap: the same value is set as <code>memory-swap</code>. The CPU limit is a
          CFS quota over a 100 ms period, and a value of 0 for either setting means unlimited.
        </p>
        <p>
          Builds are serialized: one slot for the whole instance. A deployment waiting for the slot
          shows <Term>building</Term> and nothing but its first heading in the log. The 30-minute
          deadline for a build includes that wait.
        </p>
        <BuilderPanel />
      </DocSection>

      <DocSection page="builds" id="build-secrets">
        <p>
          Your service variables are available to the build, but not as build arguments. shed writes
          each one to a private file outside the source context and passes it to BuildKit as a
          secret. A secret never lands in an image layer or in <code>docker history</code> unless
          your own build commands write it there.
        </p>
        <p>
          In a Dockerfile you opt in per instruction with{" "}
          <code>RUN --mount=type=secret,id=KEY</code>, which exposes the value as the file{" "}
          <code>/run/secrets/KEY</code> for that one command. The <Term>ARG</Term> pattern doesn't
          receive service variables. If your Dockerfile reads them with <code>ARG KEY</code>,
          migrate it to a secret mount. The variables shed injects, such as <code>PORT</code> and{" "}
          <code>SHED_GIT_COMMIT_SHA</code>, are offered the same way.
        </p>
        <SecretSnippet />
        <p>
          Build logs mask the literal values of your variables and the clone credential, even when a
          value is split across output chunks. Masking can't stop a build from deliberately
          printing, encoding, or baking in a secret it was given, so mount only what a step needs.
        </p>
      </DocSection>

      <DocSection page="builds" id="disk-guard">
        <p>
          Builds fill disks, with layers, caches, and clones. Rather than let one wedge the host,
          shed checks free space before and during every build. Two filesystems count: the one
          holding the build workspace under <code>data.dir</code>, and the one holding Docker's root
          directory.
        </p>
        <DocTable
          mono
          head={["When", "Rule", "Result"]}
          rows={[
            [
              "Before the clone",
              "free < build.min_free_mb",
              "Build fails at once with build: not enough free disk space",
            ],
            [
              "Every 3 seconds while building",
              "free < build.min_free_mb / 2",
              "Build is canceled and the deployment fails with the reason",
            ],
          ]}
        />
        <p>
          The default is 2048 MiB, so builds start with at least 2 GiB free and are canceled under 1
          GiB. A value of 0 turns both checks off. It's a best-effort monitor, not a quota: a build
          that writes faster than the poll interval can still overshoot.
        </p>
        <DiskGuard />
      </DocSection>

      <DocSection page="builds" id="images">
        <p>
          A built image is tagged <code>shed/&lt;serviceID&gt;:&lt;deploymentID&gt;</code> and
          loaded into Docker with <code>--load</code>. After each deployment goes live, shed keeps
          the five newest images of that service, and always the active one, and removes the rest.
          This is what limits how far back a{" "}
          <Link to="/docs/$slug" params={{ slug: "deployments" }} hash="rollbacks">
            rollback
          </Link>{" "}
          can reach.
        </p>
        <p>
          Services without a port get one from the image: after the build, shed saves the lowest TCP
          port in the image's <code>EXPOSE</code> list as the service's port, and from then on
          injects it as <code>PORT</code>. That only runs while the port is 0. A new repo app starts
          with 8080, so it never auto-detects; image apps start at 0.
        </p>
        <p>
          Image apps and databases don't build. shed pulls the image, resolves it to a local image
          ID, and records that ID on the deployment, so later restarts and rollbacks use that exact
          image even if the tag moves.
        </p>
      </DocSection>
    </Doc>
  );
}
