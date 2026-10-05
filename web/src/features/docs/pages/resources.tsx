import { Link } from "@tanstack/react-router";
import { Callout } from "../../../components/Misc";
import { Diagram, Edge, Node, Note, Zone } from "../diagram";
import { Doc, DocSection, DocTable, Figure } from "../kit";

export function ResourcesDoc() {
  return (
    <Doc
      slug="resources"
      lede="Volumes keep data across deployments, and every container runs under CPU, memory, and process ceilings. Both are plain Docker features that shed sets up for you."
    >
      <DocSection page="resources" id="volumes">
        <p>
          A volume is a Docker named volume, <code>shed-vol-&lt;volumeID&gt;</code>, mounted into
          the service's container at the mount path you choose. Databases get one automatically at
          their template's data directory. Because every deployment mounts the same named volume,
          data survives deploys, restarts, and the removal of old containers.
        </p>
        <Figure caption="Containers come and go with each deployment. The volume belongs to the service.">
          <VolumeDiagram />
        </Figure>
        <ul>
          <li>
            <strong>Creation.</strong> The record exists as soon as you add the volume, but Docker
            creates the volume the first time a container that mounts it starts. A mount path must
            be a clean absolute path other than <code>/</code>, and a service can mount only one
            volume per path (409 otherwise).
          </li>
          <li>
            <strong>Deleting a volume</strong> removes its Docker volume and its data. If the
            running container still has it mounted, Docker refuses, so shed deletes the record and
            removes the data after the service's next deployment detaches it. Any other Docker error
            fails the request and keeps the record so you can retry.
          </li>
          <li>
            <strong>Deleting a service or project</strong> removes its containers, then every
            volume, image, and build log it had, and a project's network last.
          </li>
          <li>
            <strong>Deploys.</strong> A service with a volume replaces its container stop-first, see{" "}
            <a href="#storage-safety">replacement storage safety</a>.
          </li>
          <li>
            <strong>Backups.</strong> Services with volumes can be archived and restored, see{" "}
            <Link to="/docs/$slug" params={{ slug: "backups" }}>
              backups
            </Link>
            .
          </li>
        </ul>
      </DocSection>

      <DocSection page="resources" id="limits">
        <p>
          Every app and database container is created with a CPU quota, a memory limit, a process
          cap, and log rotation. A new service starts with <strong>1 core</strong> and{" "}
          <strong>1 GiB</strong>. Changing a limit applies from the service's next deployment, like
          other settings.
        </p>
        <Figure caption="What shed sets on each container, in Docker HostConfig terms.">
          <LimitsDiagram />
        </Figure>
        <DocTable
          mono
          head={["Setting", "Rule"]}
          rows={[
            [
              "cpuLimit",
              "cores as a decimal; 0 is unlimited; at least 0.01 and at most the host's CPU count",
            ],
            ["memoryLimit", "bytes; 0 is unlimited; at least 64 MiB"],
            [
              "swap",
              "set equal to the memory limit, so the container gets no swap. With no memory limit it is left to Docker's default",
            ],
            ["pids", "512 per container, fixed"],
            ["logs", "json-file, 10 MiB per file, 3 files: at most about 30 MiB per container"],
          ]}
        />
        <p>
          The CPU limit is a CFS quota, not a pinned core: a container with <code>1</code> may
          spread across several cores as long as it stays within one core's worth of time. A
          container that exceeds its memory limit is killed by the kernel, and Docker restarts it
          under the <code>unless-stopped</code> policy.
        </p>
        <Callout tone="sky" title="Limits are ceilings, not reservations">
          shed does not check that your limits add up to the host. You can give ten services 1 GiB
          each on a 4 GiB server; it only becomes a problem if they all use it at once. Also leave
          room for builds, which run in their own capped builder (<code>build.memory_mb</code>,{" "}
          <code>build.cpus</code>), and for the short overlap of old and new containers during a
          zero-downtime deploy.
        </Callout>
      </DocSection>

      <DocSection page="resources" id="storage-safety">
        <p>
          Two containers writing to one database directory corrupt it, and two containers cannot
          bind the same host port. So a service with a volume, or with a{" "}
          <Link to="/docs/$slug" params={{ slug: "networking" }} hash="public-port">
            public port
          </Link>
          , never overlaps its old and new containers. shed switches it stop-first and treats
          uncertainty as a reason to stay down.
        </p>
        <Figure caption="Top: a stop-first deployment. Bottom: how a failure is unwound.">
          <StorageDiagram />
        </Figure>
        <ol>
          <li>
            Every <em>other</em> container labeled with the service, for example a failed candidate
            whose removal failed, is stopped and removed.
          </li>
          <li>The active deployment's container is stopped, with a 30 second grace period.</li>
          <li>
            Every container of the service must now be listed as stopped. If Docker inspection or
            stop fails, the deployment fails <em>before</em> the new container exists, and the
            active container is left alone.
          </li>
          <li>
            The new container starts, passes its health check, and takes the alias and routes. The
            old container stays stopped until activation, then is removed.
          </li>
        </ol>
        <p>
          If the candidate fails, shed removes it and restarts the previous container, but only once
          every other container of the service is confirmed gone, even if the deployment was
          cancelled. A failed container start counts as ambiguous, since Docker may have started it
          anyway. If removal cannot be confirmed, the previous deployment stays stopped and the
          error says so. The same rule applies to starting a service after a reboot or a backup. The
          downtime is the time from stopping the old container to the new one passing its health
          check.
        </p>
      </DocSection>
    </Doc>
  );
}

function VolumeDiagram() {
  return (
    <Diagram
      width={800}
      height={230}
      label="Containers of successive deployments mount the same volume"
    >
      <Node x={10} y={30} w={200} title="deployment 1" sub="shed-svc-dep1 · removed" dim />
      <Node
        x={10}
        y={100}
        w={200}
        title="deployment 2"
        sub="shed-svc-dep2 · active"
        tone="grape"
        emphasis
      />
      <Node x={10} y={170} w={200} title="deployment 3" sub="candidate, next" tone="grape" dim />
      <Zone x={330} y={44} w={460} h={140} label="Docker" />
      <Node
        x={350}
        y={90}
        w={420}
        title="shed-vol-<volumeID>"
        sub="mounted at the mount path"
        tone="accent"
        emphasis
      />
      <Edge
        points={[
          [210, 56],
          [270, 56],
          [270, 116],
          [350, 116],
        ]}
        dashed
      />
      <Edge
        points={[
          [210, 126],
          [350, 126],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [210, 196],
          [270, 196],
          [270, 136],
          [350, 136],
        ]}
        dashed
      />
      <Note x={350} y={214}>
        The volume outlives every container, and is removed only when you delete it.
      </Note>
    </Diagram>
  );
}

function LimitsDiagram() {
  const items = [
    ["CPU quota", "cpuLimit · 0 = none"],
    ["Memory", "memoryLimit · 64 MiB+"],
    ["Swap", "= memory, none extra"],
    ["Processes", "512 pids, fixed"],
    ["Log files", "10 MiB x 3 files"],
  ] as const;
  return (
    <Diagram width={800} height={110} label="Limits shed sets on every container">
      <Zone x={0} y={0} w={800} h={110} label="every app and database container" tone="grape" />
      {items.map(([title, sub], i) => (
        <Node key={title} x={12 + i * 156} y={40} w={146} title={title} sub={sub} />
      ))}
    </Diagram>
  );
}

function StorageDiagram() {
  const xs = [0, 166, 332, 498, 664];
  const forward = [
    ["Clear strays", "other containers"],
    ["Stop previous", "graceful, 30s"],
    ["Confirm stopped", "all must be idle"],
    ["Start candidate", "no alias yet"],
    ["Health + switch", "alias, routes"],
  ] as const;
  const back = [
    [498, "Remove candidate", "failure lands here"],
    [332, "Clear strays", "all but previous"],
    [166, "Restart previous", "only if confirmed"],
    [0, "Serving again", "routes unchanged"],
  ] as const;
  return (
    <Diagram width={800} height={236} label="Stop-first replacement and failure recovery">
      {forward.map(([title, sub], i) => (
        <Node key={title} x={xs[i]!} y={20} w={136} title={title} sub={sub} emphasis={i === 1} />
      ))}
      {xs.slice(0, -1).map((x) => (
        <Edge
          key={x}
          points={[
            [x + 136, 46],
            [x + 166, 46],
          ]}
        />
      ))}
      {back.map(([x, title, sub]) => (
        <Node key={title} x={x} y={150} w={136} title={title} sub={sub} dim />
      ))}
      <Edge
        points={[
          [566, 72],
          [566, 150],
        ]}
        dashed
        label="fails"
        labelAt={[590, 118]}
      />
      {[498, 332, 166].map((x) => (
        <Edge
          key={x}
          points={[
            [x, 176],
            [x - 30, 176],
          ]}
          dashed
        />
      ))}
      <Note x={0} y={122}>
        Recovery
      </Note>
      <Note x={0} y={224}>
        If the replacement cannot be confirmed removed, the previous container stays stopped.
      </Note>
    </Diagram>
  );
}
