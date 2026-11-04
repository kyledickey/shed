import { Button, SkeletonLine } from "@cloudflare/kumo";
import { CheckIcon, XIcon } from "@phosphor-icons/react";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { meQuery } from "../../api/auth";
import { errorMessage, isApiError } from "../../api/client";
import { oauthRequestQuery, useDecideOAuthRequest } from "../../api/oauth";
import type { OAuthRequest } from "../../api/types";
import { AuthCard, authButtonClass, authErrorClass } from "./AuthCard";
import styles from "./Consent.module.css";

const expiredText =
  "This sign-in request expired or was already used. Start connecting again from your agent.";

/** Consent asks the signed-in user to approve or deny an MCP client's authorization request. */
export function Consent({ requestId }: { requestId: string | undefined }) {
  const request = useQuery({ ...oauthRequestQuery(requestId ?? ""), enabled: !!requestId });

  if (!requestId || isApiError(request.error, 404)) return <Expired />;
  if (request.error) {
    return (
      <AuthCard title="Authorize agent" description="The request couldn't be loaded.">
        <p className={authErrorClass} role="alert">
          {errorMessage(request.error)}
        </p>
      </AuthCard>
    );
  }
  if (!request.data) {
    return (
      <AuthCard title="Authorize agent" description="Loading the request…">
        <div className={styles.loading} aria-busy>
          <SkeletonLine minWidth={50} maxWidth={80} />
          <SkeletonLine minWidth={40} maxWidth={70} />
          <SkeletonLine minWidth={30} maxWidth={60} />
        </div>
      </AuthCard>
    );
  }
  return <ConsentForm request={request.data} />;
}

function Expired() {
  const navigate = useNavigate();
  return (
    <AuthCard title="Request expired" description={expiredText}>
      <Button size="lg" className={authButtonClass} onClick={() => void navigate({ to: "/" })}>
        Go to the dashboard
      </Button>
    </AuthCard>
  );
}

const loopbackHost = /^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/;

function ConsentForm({ request }: { request: OAuthRequest }) {
  const { data: me } = useSuspenseQuery(meQuery);
  const decide = useDecideOAuthRequest(request.id);
  const name = request.clientName || "An unnamed client";
  const extraScopes = request.scopes.filter((s) => s !== "read");

  if (isApiError(decide.error, 404)) return <Expired />;

  const submit = (approve: boolean) =>
    decide.mutate(approve, { onSuccess: ({ redirect }) => window.location.assign(redirect) });
  const leaving = decide.isSuccess;

  return (
    <AuthCard
      title="Authorize agent"
      description={
        <>
          <strong>{name}</strong> wants read-only access to this shed through MCP.
        </>
      }
    >
      <dl className={styles.facts}>
        <div>
          <dt>Client</dt>
          <dd>
            <span>
              {name}
              {request.clientUri && (
                <>
                  {" "}
                  <span className={styles.muted}>{request.clientUri}</span>
                </>
              )}
            </span>
          </dd>
        </div>
        <div>
          <dt>Returns to</dt>
          <dd>
            <span className={styles.mono}>{request.redirectHost}</span>
            {loopbackHost.test(request.redirectHost) && (
              <span className={styles.muted}>an app on your computer</span>
            )}
          </dd>
        </div>
        <div>
          <dt>Acting as</dt>
          <dd>
            {me.avatarUrl && <img className={styles.avatar} src={me.avatarUrl} alt="" />}
            <span>{me.login}</span>
          </dd>
        </div>
      </dl>

      <div className={styles.scope}>
        <span className={styles.scopeTitle}>It will be able to</span>
        <ul className={styles.items}>
          <li>
            <CheckIcon size={14} className={styles.glyph} />
            Read projects, services, deployments, and their settings
          </li>
          <li>
            <CheckIcon size={14} className={styles.glyph} />
            Read build logs, runtime logs, metrics, and shed's own log
          </li>
          <li>
            <CheckIcon size={14} className={styles.glyph} />
            See variable names, backups, update status, and your GitHub repositories
          </li>
          {extraScopes.map((s) => (
            <li key={s}>
              <CheckIcon size={14} className={styles.glyph} />
              <span className={styles.mono}>{s}</span>
            </li>
          ))}
        </ul>
        <span className={styles.scopeTitle}>It won't be able to</span>
        <ul className={styles.items}>
          <li>
            <XIcon size={14} className={styles.glyph} />
            See variable values or secrets
          </li>
          <li>
            <XIcon size={14} className={styles.glyph} />
            Deploy, change, or delete anything
          </li>
        </ul>
      </div>

      {decide.error && (
        <p className={authErrorClass} role="alert">
          {errorMessage(decide.error)}
        </p>
      )}

      <div className={styles.actions}>
        <Button
          size="lg"
          className={authButtonClass}
          disabled={leaving || decide.isPending}
          loading={decide.isPending && decide.variables === false}
          onClick={() => submit(false)}
        >
          Deny
        </Button>
        <Button
          variant="primary"
          size="lg"
          className={authButtonClass}
          disabled={leaving || decide.isPending}
          loading={decide.isPending && decide.variables === true}
          onClick={() => submit(true)}
        >
          Approve
        </Button>
      </div>
      <p className={styles.note}>
        {leaving
          ? `Returning to ${name}…`
          : "The client chose its own name, so only approve a connection you just started. Revoke it anytime in Settings."}
      </p>
    </AuthCard>
  );
}
