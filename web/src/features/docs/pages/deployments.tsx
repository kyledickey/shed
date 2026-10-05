import { Link } from "@tanstack/react-router";
import { Doc, DocSection, DocTable, Figure, Term } from "../kit";
import { LogAnatomy } from "./deployments/LogAnatomy";
import { LogReader } from "./deployments/LogReader";
import { PipelineDiagram } from "./deployments/PipelineDiagram";
import { StatusMachine } from "./deployments/StatusMachine";
import { Switchover } from "./deployments/Switchover";

export function DeploymentsDoc() {
  return (
    <Doc
      slug="deployments"
      lede="A deployment is one attempt to get a commit or an image running. It builds, starts a new container beside the old one, checks it is healthy, then moves traffic over. If anything fails, the old container keeps serving."
    >
      <DocSection page="deployments" id="pipeline">
        <p>
          Each service has one worker, so its deployments run one at a time. A new deployment
          cancels any older one that hasn't finished: a queued one is canceled outright, a running
          one is interrupted with the reason <code>superseded by a newer deployment</code>.
          Deployments of different services run in parallel, though only one build runs at a time
          across the whole instance.
        </p>
        <Figure caption="Solid arrows are the happy path. Dashed arrows are failure exits.">
          <PipelineDiagram />
        </Figure>
        <ol>
          <li>
            <strong>Wait for CI.</strong> Only for apps with <Term>waitForCi</Term> and a commit.
            See{" "}
            <Link to="/docs/$slug" params={{ slug: "github" }} hash="wait-for-ci">
              Waiting for CI
            </Link>
            .
          </li>
          <li>
            <strong>Build.</strong> Repo apps are cloned at the commit and built into{" "}
            <code>shed/&lt;serviceID&gt;:&lt;deploymentID&gt;</code>. Image apps and databases pull
            their image and record its local image ID. See{" "}
            <Link to="/docs/$slug" params={{ slug: "builds" }} hash="detection">
              Builds
            </Link>
            .
          </li>
          <li>
            <strong>Start.</strong> Variables are resolved and the container is created without the
            service's network alias.
          </li>
          <li>
            <strong>Health check.</strong> Up to 120 seconds for a TCP connect or a 2xx/3xx on the
            health check path.
          </li>
          <li>
            <strong>Switch.</strong> The alias moves, a second probe runs, routes are applied, the
            deployment turns <Term>active</Term>, and the old container is removed.
          </li>
        </ol>
      </DocSection>

      <DocSection page="deployments" id="statuses">
        <p>
          A deployment's status only moves forward. Four statuses mean it is still in the pipeline;
          the rest are final, and a final deployment never changes status again, except that{" "}
          <Term>active</Term> becomes <Term>removed</Term> when the next one goes live.
        </p>
        <Figure caption="Dashed transitions end the deployment without it going live.">
          <StatusMachine />
        </Figure>
        <DocTable
          mono
          head={["Status", "Meaning"]}
          rows={[
            ["queued", "Created and waiting for its service's worker."],
            ["waiting", "Holding for the commit's CI results."],
            ["building", "Cloning and building, or pulling the image."],
            ["deploying", "Starting, health checking, or switching traffic."],
            ["active", "Live. A service has at most one, enforced by the database."],
            ["removed", "Was active; a newer deployment replaced it."],
            ["failed", "Ended with an error, recorded on the deployment."],
            [
              "canceled",
              "Stopped by a newer deployment, the Cancel button, stopping the service, or deleting it.",
            ],
            ["skipped", "CI failed, so the deployment never built."],
            [
              "crashed",
              "Reserved. shed doesn't record it today; a container that dies shows up as a crashed service.",
            ],
          ]}
        />
      </DocSection>

      <DocSection page="deployments" id="zero-downtime">
        <p>
          Replacing a container without dropping requests is mostly about ordering. shed never
          routes to a container it hasn't verified twice, and never retires the old one before the
          new one is in the routing table.
        </p>
        <p>
          Services that can share nothing, those with volumes or a public port, can't overlap: two
          Postgres containers can't open one data directory, and two containers can't publish one
          host port. For them shed stops the old container first and accepts a short outage.
          Everything else overlaps.
        </p>
        <Switchover />
        <p>
          If any step fails, the candidate is removed and the deployment is marked{" "}
          <Term>failed</Term>. When the old container had been stopped first, shed starts it again,
          but only after confirming that every other container of the service is gone, since two of
          them could share storage. If that can't be confirmed the old one stays stopped, and the
          error says so.
        </p>
        <p>
          Route updates are serialized through activation. If Caddy rejects the new config, the
          candidate fails and the previous deployment keeps its routes. Once routing has succeeded,
          activation finishes even if you cancel at that moment, so a routed candidate is never
          removed mid-switch.
        </p>
      </DocSection>

      <DocSection page="deployments" id="build-log">
        <p>
          Every deployment writes a build log to{" "}
          <code>&lt;data&gt;/logs/&lt;deploymentID&gt;.log</code>, whatever its outcome. Each stage
          prints a line starting with <code>==&gt; </code>, followed by detail lines. The headings
          are stable, which makes them easy to grep.
        </p>
        <LogAnatomy />
        <p>
          Output from the container itself is copied in from the moment it starts until the health
          check ends, without Docker's timestamps, so its boot messages and last words are right
          there. A container line that begins with <code>==&gt; </code> gets a leading space, so it
          can't be mistaken for a heading. Logs are capped at <code>deployments.log_max_mb</code>{" "}
          (10 by default); past the cap, output is dropped and the log ends with{" "}
          <code>==&gt; Log truncated</code>. Values of the service's variables are masked.
        </p>
        <p>
          Paste a log into the reader below to see which stage it reached. A deployment that failed
          is shown stopped at the last stage that printed a heading.
        </p>
        <LogReader />
      </DocSection>

      <DocSection page="deployments" id="rollbacks">
        <p>
          Redeploying an old deployment is how rollbacks work. It creates a new deployment with the
          trigger <Term>redeploy</Term>, the same commit details, and the same recorded image, then
          runs it from the start stage: no CI wait and no build. The old deployment isn't revived,
          so your history keeps reading in order.
        </p>
        <p>
          A redeploy reuses the image but not the configuration. It starts with the service's{" "}
          <em>current</em> variables, port, and limits. Rolling back code doesn't roll back
          settings.
        </p>
        <p>
          The image must be named immutably. Built images (
          <code>shed/&lt;serviceID&gt;:&lt;deploymentID&gt;</code>) and pulled images pinned by ID
          or digest qualify. Older records that stored only a mutable tag such as{" "}
          <code>postgres:18-alpine</code> are refused, since the tag may have moved; deploy those
          afresh. shed also keeps only the five newest built images per service, so deployments
          older than that usually can't start.
        </p>
      </DocSection>

      <DocSection page="deployments" id="history">
        <p>
          After each deployment ends, shed trims the service's history: finished deployments (
          <Term>failed</Term>, <Term>removed</Term>, <Term>canceled</Term>, <Term>skipped</Term>)
          beyond the newest <code>deployments.keep</code> are deleted, along with their build logs.
          The default is 50; <code>0</code> keeps everything. Active, crashed, and in-progress
          deployments are never deleted, however old.
        </p>
        <p>
          The dashboard and <code>GET /api/services/&#123;id&#125;/deployments</code> list the
          newest 50.
        </p>
      </DocSection>

      <DocSection page="deployments" id="stopping">
        <p>
          Stop, start, and restart act on the active deployment's container. They aren't deployments
          and write no build log.
        </p>
        <DocTable
          mono
          head={["Action", "What happens"]}
          rows={[
            [
              "stop",
              "Cancels any deployment in progress (canceled, \"service stopped\"), sets the stopped flag, stops the container with a 30-second grace period but doesn't remove it, and drops the service's routes. The deployment stays active.",
            ],
            [
              "start",
              "Clears the flag, starts the active deployment's container (recreating it from the recorded image and port if it's gone), and restores routes. Refused if nothing was ever deployed.",
            ],
            [
              "restart",
              "Restarts the active container, then reapplies routes, because the container may come back at a different address. Refused if the service is stopped or has nothing deployed.",
            ],
          ]}
        />
        <p>
          A stopped service stays stopped across a shed restart or reboot. You can still deploy it:
          when the new deployment goes live, it clears the flag. All three are refused with a
          conflict while the service is fenced by a restore or held for a backup.
        </p>
      </DocSection>

      <DocSection page="deployments" id="reconcile">
        <p>
          Containers use the <code>unless-stopped</code> restart policy, so Docker brings them back
          after a reboot by itself. shed still checks its own books on every start, because a crash
          can leave the database and Docker disagreeing. Before it accepts requests, shed does four
          things:
        </p>
        <ol>
          <li>
            Marks every deployment still <Term>queued</Term>, <Term>waiting</Term>,{" "}
            <Term>building</Term>, or <Term>deploying</Term> as <Term>failed</Term> with the error{" "}
            <code>interrupted by restart</code>, and removes its containers. Pipelines don't resume;
            deploy again.
          </li>
          <li>
            Ensures the active deployment of every service that is not stopped and not fenced has a
            running container, recreating a missing one from its recorded image and port.
          </li>
          <li>
            Keeps one deployment per service, the newest active one, and removes the service's other
            containers. That is how a predecessor left behind by a crash mid-switchover disappears.
          </li>
          <li>Applies the proxy routes.</li>
        </ol>
        <p>
          A graceful shutdown works the same way from the other side: running pipelines are
          interrupted and recorded with the error <code>interrupted by shutdown</code>.
        </p>
      </DocSection>
    </Doc>
  );
}
