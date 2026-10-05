import { Link } from "@tanstack/react-router";
import { Doc, DocSection, DocTable, Figure } from "../kit";
import { MiddlewareDiagram, SignInDiagram, WebhookDiagram } from "./security/diagrams";
import { TokenBucketSim } from "./security/TokenBucketSim";

export function SecurityDoc() {
  return (
    <Doc
      slug="security"
      lede="shed has one kind of user: a GitHub account you named in the config file. Everything else is about keeping that boundary tight, from the sign-in flow to the headers on every response."
    >
      <DocSection page="security" id="authentication">
        <p>
          You sign in with GitHub through the same App that handles repositories and webhooks. shed
          never sees a password. Access is decided by one list in <code>shed.toml</code>:
        </p>
        <Figure caption="Steps 3 and 4 happen entirely between your browser and GitHub.">
          <SignInDiagram />
        </Figure>
        <ol>
          <li>
            <code>GET /api/auth/login</code> sets a state cookie, <code>shed_oauth_state</code> (16
            random bytes, valid for 10 minutes, scoped to <code>/api/auth</code>), and redirects to
            GitHub's authorize URL carrying the same state.
          </li>
          <li>
            GitHub sends the browser to <code>/api/auth/callback</code> with a code and the state.
            shed compares the state with the cookie in constant time, and clears the cookie either
            way. A mismatch is a 400. A missing code, because you declined, redirects to{" "}
            <code>/login?error=denied</code>.
          </li>
          <li>
            shed exchanges the code for your GitHub profile. A failed exchange redirects to{" "}
            <code>/login?error=failed</code>.
          </li>
          <li>
            Your login is checked against <code>auth.allowed_users</code>, case-insensitively.{" "}
            <strong>An empty list denies everyone.</strong> If you're on it, shed saves your
            profile, creates a session, sets the cookie, and redirects to <code>/</code>. Otherwise
            you land on <code>/login?error=denied</code>.
          </li>
        </ol>
        <p>
          The rule is enforced again on every request, not just at sign-in. At startup shed also
          permanently deletes the sessions of logins that are no longer on the list. To revoke
          someone, remove them from <code>allowed_users</code> and restart shed, which also ends
          their open log streams. See{" "}
          <Link to="/docs/$slug" params={{ slug: "configuration" }} hash="env-overrides">
            environment overrides
          </Link>{" "}
          for setting the list without editing the file.
        </p>
      </DocSection>

      <DocSection page="security" id="sessions">
        <p>
          A session is a random token in a cookie. shed stores only its hash, so a copy of{" "}
          <code>shed.db</code> or a backup does not contain anything a browser could present.
        </p>
        <DocTable
          head={["Property", "shed_session", "shed_oauth_state"]}
          rows={[
            ["Value", "32 random bytes, base64url", "16 random bytes, base64url"],
            [
              "Stored server-side as",
              "sha256 hex in sessions.token_hash",
              "nothing; compared to the query",
            ],
            ["Lifetime", "30 days, fixed at sign-in", "10 minutes"],
            ["Path", "/", "/api/auth"],
            ["HttpOnly", "yes", "yes"],
            ["SameSite", "Lax", "Lax"],
            ["Secure", "when server.url is https", "when server.url is https"],
          ]}
        />
        <p>
          The lifetime does not slide: you sign in again every 30 days. Logout (
          <code>POST /api/auth/logout</code>) deletes the session row and clears the cookie, and it
          has the same origin and JSON checks as any other mutation.
        </p>
      </DocSection>

      <DocSection page="security" id="requests">
        <p>
          Every API route that needs a session runs through the same chain, outermost first. Order
          matters: an unauthenticated mutation gets 401, not 403.
        </p>
        <Figure caption="The status each layer returns when it rejects a request.">
          <MiddlewareDiagram />
        </Figure>
        <ul>
          <li>
            <strong>Origin check.</strong> For POST, PUT, PATCH, and DELETE, an <code>Origin</code>{" "}
            header must equal the origin of <code>server.url</code>. With no <code>Origin</code>, a{" "}
            <code>Sec-Fetch-Site</code> header, if present, must be <code>same-origin</code>.
            Anything else is a 403 <code>untrusted request origin</code>. Sibling apps on your other
            domains are different origins, so they can't drive the API even with your cookie.
            Clients that send neither header, like <code>curl</code>, pass this check and rely on
            the cookie. Reads (GET, HEAD, OPTIONS) skip it.
          </li>
          <li>
            <strong>JSON only.</strong> POST, PUT, and PATCH must declare{" "}
            <code>Content-Type: application/json</code>, even with an empty body, or get 415.
            Browsers can't send that type cross-site without a CORS preflight, which backs up the{" "}
            <code>SameSite=Lax</code> cookie. Request bodies are capped at 1 MiB.
          </li>
          <li>
            <strong>Security headers.</strong> Every response from shed's handler, API and
            dashboard, carries the four headers below, so the dashboard can't be framed by another
            site.
          </li>
        </ul>
        <p>
          For Vite development, set <code>server.url</code> to the dev server's origin so the origin
          check passes.
        </p>
        <DocTable
          mono
          head={["Header", "Value", "Effect"]}
          rows={[
            [
              "Content-Security-Policy",
              "frame-ancestors 'none'",
              "No other site can embed the dashboard in a frame.",
            ],
            ["X-Frame-Options", "DENY", "The same rule for older browsers."],
            ["X-Content-Type-Options", "nosniff", "Browsers must trust the declared content type."],
            [
              "Referrer-Policy",
              "same-origin",
              "Other sites never receive a referrer from the dashboard.",
            ],
          ]}
        />
        <p>
          Contributors: the request checks are implemented in the{" "}
          <Link to="/docs/$slug" params={{ slug: "internals" }} hash="http">
            HTTP layer
          </Link>
          .
        </p>
      </DocSection>

      <DocSection page="security" id="secrets">
        <p>
          shed keeps secrets in as few places as it can, and keeps them out of everything that is
          displayed or logged.
        </p>
        <DocTable
          head={["Secret", "Where it lives", "How it is protected"]}
          rows={[
            [
              "Service variables",
              "variables table in shed.db, in plain text",
              "Supplied to builds as BuildKit secrets in private files outside the source, never as build arguments. Dockerfiles read them with RUN --mount=type=secret.",
            ],
            [
              "GitHub App credentials",
              "settings table: client secret, webhook secret, private key",
              "Never returned by the API.",
            ],
            [
              "Git clone credentials",
              "Short-lived installation tokens",
              "Passed as a scoped HTTP authorization header in the child process environment, not on a command line or in repository config.",
            ],
            [
              "age secret key",
              "settings table, backup.age_identity",
              "Only GET /api/backups/settings/key returns it, on demand.",
            ],
            [
              "S3 secret access key",
              "backup_destinations table",
              "The API returns hasSecret, never the secret. Saving with a blank secret keeps the stored one.",
            ],
            [
              "Session tokens",
              "Only their sha256 in sessions",
              "A database copy can't be replayed as a cookie.",
            ],
            [
              "Database passwords during backups",
              "The container's own environment",
              "Passed as PGPASSWORD, MYSQL_PWD, REDISCLI_AUTH, or a private --config file, never on a command line.",
            ],
          ]}
        />
        <h3>Logs</h3>
        <p>
          Build logs mask the literal values of the service's variables and of clone credentials,
          including values split across writes. Container startup and runtime log streams mask the
          literal resolved values of stored variables the same way. Two limits are worth knowing.
          Masking uses the variable configuration at the time, so changing a variable can't scrub
          older saved logs. And a build can still embed or encode a secret it receives, so secret
          mounts don't make an untrusted build script safe.
        </p>
        <p>
          Because <code>shed.db</code> holds most of the secrets above in plain text, treat the disk
          as sensitive, and turn on{" "}
          <Link to="/docs/$slug" params={{ slug: "backups" }} hash="encryption">
            backup encryption
          </Link>{" "}
          so its backups are not readable in your bucket.
        </p>
      </DocSection>

      <DocSection page="security" id="webhook-limits">
        <p>
          <code>POST /api/github/webhook</code> is the one route that is public and writes. GitHub
          can't present a session, so it is authenticated by an HMAC-SHA256 signature of the body,
          and bounded by a budget so a flood can't exhaust the host.
        </p>
        <Figure caption="Rejected deliveries never reach the deploy pipeline.">
          <WebhookDiagram />
        </Figure>
        <DocTable
          head={["Limit", "Value", "Over the limit"]}
          rows={[
            ["Signature header", "sha256= plus 64 hex digits", "401, before the body is read"],
            ["Concurrent deliveries", "4", "429"],
            ["Rate", "burst of 8, refilled at 1 per second", "429"],
            ["Body size", "1 MiB", "413"],
            ["Body read time", "10 s deadline", "Read fails"],
            ["Signature", "HMAC-SHA256 of the body with the webhook secret", "401"],
            [
              "Dedup cache",
              "1,024 entries, 24 hours, in memory",
              "Oldest completed entries are evicted",
            ],
          ]}
        />
        <p>
          Only the shape of the signature header can be checked before the body is read, because the
          HMAC covers the body. The guard is what bounds an unsigned sender: at most four bodies of
          at most 1 MiB are in flight, and at most one new delivery per second after the burst. The
          signature is verified with a constant-time comparison before anything is parsed.
        </p>
        <p>
          After the signature passes, only <code>push</code> events matter, and others get 202. For
          a push to <code>refs/heads/&lt;branch&gt;</code>, every app service with the same repo and
          branch and <code>auto_deploy</code> on gets a <code>push</code> deployment. Each payload
          is hashed, and a repeated delivery to the same service is skipped, so GitHub's
          redeliveries don't deploy twice. A delivery that is mid-scheduling, or that fails to
          enqueue, answers 503 so it can be retried safely. The cache is cleared on restart. Pushes
          bigger than 1 MiB exceed the budget and need a manual deploy.
        </p>
        <TokenBucketSim />
      </DocSection>
    </Doc>
  );
}
