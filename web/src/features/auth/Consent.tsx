import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check, CircleX, X } from "lucide-react";
import { meQuery } from "../../api/auth";
import { errorMessage, isApiError } from "../../api/client";
import { oauthRequestQuery, useDecideOAuthRequest } from "../../api/oauth";
import type { OAuthRequest } from "../../api/types";
import { Button, buttonClass } from "../../components/Button";
import { Avatar, Callout, Skeleton } from "../../components/Misc";
import { AuthCard } from "./AuthCard";
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
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {errorMessage(request.error)}
        </Callout>
      </AuthCard>
    );
  }
  if (!request.data) {
    return (
      <AuthCard title="Authorize agent" description={<Skeleton width="80%" height={14} />}>
        <Skeleton height={140} radius={10} />
      </AuthCard>
    );
  }
  return <ConsentForm request={request.data} />;
}

function Expired() {
  return (
    <AuthCard title="Request expired" description={expiredText}>
      <Link to="/" className={buttonClass({ size: "lg" })}>
        Go to the dashboard
      </Link>
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
            <Avatar src={me.avatarUrl} name={me.login} size={18} />
            <span>{me.login}</span>
          </dd>
        </div>
      </dl>

      <div className={styles.scope}>
        <span className={styles.scopeTitle}>It will be able to</span>
        <ul className={styles.items}>
          <li>
            <Check size={14} className={styles.yes} />
            Read projects, services, deployments, and their settings
          </li>
          <li>
            <Check size={14} className={styles.yes} />
            Read build logs, runtime logs, metrics, and shed's own log
          </li>
          <li>
            <Check size={14} className={styles.yes} />
            See variable names, backups, update status, and your GitHub repositories
          </li>
          {extraScopes.map((s) => (
            <li key={s}>
              <Check size={14} className={styles.yes} />
              <span className={styles.mono}>{s}</span>
            </li>
          ))}
        </ul>
        <span className={styles.scopeTitle}>It won't be able to</span>
        <ul className={styles.items}>
          <li>
            <X size={14} className={styles.no} />
            See variable values or secrets
          </li>
          <li>
            <X size={14} className={styles.no} />
            Deploy, change, or delete anything
          </li>
        </ul>
      </div>

      {decide.error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {errorMessage(decide.error)}
        </Callout>
      )}

      <div className={styles.actions}>
        <Button
          size="lg"
          disabled={leaving || decide.isPending}
          loading={decide.isPending && decide.variables === false}
          onClick={() => submit(false)}
        >
          Deny
        </Button>
        <Button
          variant="primary"
          size="lg"
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
