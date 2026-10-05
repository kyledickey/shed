import { Link } from "@tanstack/react-router";
import { CodeBlock, Doc, DocSection, DocTable, Figure, Term } from "../kit";
import { AppDiagram } from "./github/AppDiagram";
import { CiEvaluator } from "./github/CiEvaluator";
import { SetupSequence } from "./github/SetupSequence";
import { WebhookFlow } from "./github/WebhookFlow";

export function GithubDoc() {
  return (
    <Doc
      slug="github"
      lede="shed uses one GitHub App for everything it needs from GitHub: signing you in, reading your repositories, hearing about pushes, and checking CI. You create it once, from the dashboard, in a couple of clicks."
    >
      <DocSection page="github" id="github-app">
        <p>
          A GitHub App is an identity with its own permissions, installed on the accounts and
          organizations whose repositories you want to deploy. shed asks for the least it needs, all
          read-only: <Term>contents</Term>, <Term>metadata</Term>, <Term>checks</Term>, and{" "}
          <Term>statuses</Term>, plus the <Term>push</Term> event. It never writes to your
          repositories.
        </p>
        <Figure caption="One App, four jobs.">
          <AppDiagram />
        </Figure>
        <p>
          The App's credentials (app ID, slug, client ID and secret, webhook secret, and private
          key) live in shed's database, not in <code>shed.toml</code>. shed signs a short-lived JWT
          with the private key to act as the App, and trades it for installation tokens to clone and
          to read CI. Which repositories shed can see is decided on GitHub, by where you install the
          App and which repositories you grant it, never by who is signed in.
        </p>
      </DocSection>

      <DocSection page="github" id="setup">
        <p>
          On first run there is no App. shed logs a warning containing the setup URL and a one-time
          token, and the dashboard sends you to <code>/setup</code>. The token proves you can read
          the server's log, since nobody else can create the App. It is regenerated at every boot
          until setup finishes, then discarded.
        </p>
        <Figure caption="The manifest flow. Steps in the middle lane are shed; the rest happen in your browser and on GitHub.">
          <SetupSequence />
        </Figure>
        <p>
          The manifest describes the App completely, so you don't fill in a GitHub form: its URL and
          callback are derived from <code>server.url</code>, the webhook points at{" "}
          <code>/api/github/webhook</code>, and the App is created <em>public</em> so you can
          install it on organizations as well as your own account. Its name is derived from the
          dashboard host and capped at GitHub's 34 characters, for example{" "}
          <code>shed-shed-example-com</code>. After conversion shed redirects you to GitHub's
          install page for the new App. If anything goes wrong the browser returns to{" "}
          <code>/setup?error=…</code> with <code>invalid_token</code>,{" "}
          <code>already_configured</code>, or <code>failed</code>.
        </p>
        <h3>Reusing an existing App</h3>
        <p>
          If you lost the database but not the App, import it instead. Generate a client secret, a
          private key, and a webhook secret in the App's GitHub settings, and give them to the setup
          page with the token. shed signs a JWT with the key and calls <code>GET /app</code> to
          confirm the ID, key, and client ID belong together, and learns the slug from the answer.
          GitHub offers no way to check the client secret or the webhook secret, so a typo in those
          only shows up later, as a failed sign-in or rejected webhooks. The App's webhook and
          callback URLs must already point at <code>server.url</code>.
        </p>
      </DocSection>

      <DocSection page="github" id="sign-in">
        <p>
          Signing in is the standard OAuth web flow, through the same App.{" "}
          <code>/api/auth/login</code> redirects to GitHub with the App's client ID and a random
          state cookie. GitHub returns to <code>/api/auth/callback</code>, where shed exchanges the
          code, reads your profile, and checks your login against the list in its config.
        </p>
        <CodeBlock title="/etc/shed/shed.toml" lang="toml">
          {`[auth]
allowed_users = ["your-login"]  # case-insensitive; an empty list denies everyone`}
        </CodeBlock>
        <p>
          That list is the only access control: there are no roles, so everyone on it can do
          everything. The rule is checked on every request, not just at sign-in. When shed starts it
          permanently revokes the sessions of logins that are no longer listed, so to remove
          someone, edit the config and restart, which also closes their open streams.
        </p>
        <p>
          A successful sign-in sets the <code>shed_session</code> cookie: 32 random bytes of which
          only a hash is stored, <code>HttpOnly</code>, <code>SameSite=Lax</code>,{" "}
          <code>Secure</code> when <code>server.url</code> is https, valid for 30 days. More in{" "}
          <Link to="/docs/$slug" params={{ slug: "security" }} hash="sessions">
            Sessions
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="github" id="webhooks">
        <p>
          When you push, GitHub POSTs to <code>/api/github/webhook</code>. This endpoint takes no
          session; its only credential is an HMAC-SHA256 signature of the body with the App's
          webhook secret. shed rejects a malformed signature before it reads a byte of the body, and
          applies a concurrency cap and a token bucket before doing any hashing.
        </p>
        <Figure caption="Checks run top to bottom. The right-hand column is what GitHub sees when one fails.">
          <WebhookFlow />
        </Figure>
        <p>
          A push to <code>refs/heads/&lt;branch&gt;</code> deploys every <Term>app</Term> service
          whose <Term>repo</Term> and <Term>branch</Term> match and whose <Term>autoDeploy</Term> is
          on. The repo matches case-insensitively, as GitHub treats it; the branch matches exactly.
          Each deployment gets the trigger <Term>push</Term> and the head commit's SHA, first
          message line, and author.
        </p>
        <DocTable
          mono
          head={["Response", "When"]}
          rows={[
            ["409", "The GitHub App isn't configured yet."],
            ["401", "The signature is missing, malformed, or doesn't match."],
            [
              "429",
              "More than 4 deliveries in flight, or the bucket (burst of 8, then 1 per second) is empty.",
            ],
            ["413", "The payload is over 1 MiB. Deploy manually."],
            ["400", "The body can't be read or parsed."],
            [
              "202",
              "Accepted. Also returned for events other than push, deleted branches, tags, and pushes nothing tracks.",
            ],
            [
              "503",
              "A delivery is still being scheduled, or enqueueing failed, including a service held for a backup. GitHub can redeliver.",
            ],
          ]}
        />
        <p>
          Deliveries are deduplicated per service by a hash of the payload, for up to 24 hours and
          1,024 entries in memory, cleared on restart. A redelivery after a 503 therefore deploys
          only the services that missed out the first time. A service fenced by a failed restore is
          skipped, with a log line, and doesn't make the delivery fail.
        </p>
      </DocSection>

      <DocSection page="github" id="wait-for-ci">
        <p>
          With <Term>waitForCi</Term> on, a deployment with a commit holds in <Term>waiting</Term>{" "}
          until the commit's checks pass. This is how you keep a red build from reaching production
          when your CI runs on GitHub Actions or anything else that reports checks or commit
          statuses. Manual deployments of a branch head wait too; redeploys don't, since they reuse
          an image that already passed.
        </p>
        <p>
          Every 10 seconds shed reads the commit's check runs and its combined commit status and
          folds them into one state. A completed check run passes if its conclusion is success,
          neutral, or skipped; anything else, including cancelled and timed out, fails. A failure
          outranks anything still pending, so a red check ends the wait immediately.
        </p>
        <CiEvaluator />
        <p>
          Two edge cases matter. First, no checks isn't approval: shed keeps waiting until some
          appear and pass, because a workflow may not have registered yet. If your repository has no
          CI at all, a service with this setting never deploys, so leave it off. Second, errors
          reaching GitHub don't end the wait; they're logged to the build log as{" "}
          <code>Checking CI status: …</code> and polling continues.
        </p>
        <DocTable
          mono
          head={["Outcome", "Deployment"]}
          rows={[
            ["All checks pass", "Continues with the build."],
            ["Any check fails", 'skipped, with the error "CI failed". Nothing was built.'],
            [
              "Still pending after 60 minutes",
              'failed, with "timed out after 1h0m0s waiting for CI".',
            ],
            [
              "A newer push arrives",
              'canceled, "superseded by a newer deployment". The newer one starts its own wait.',
            ],
          ]}
        />
      </DocSection>
    </Doc>
  );
}
