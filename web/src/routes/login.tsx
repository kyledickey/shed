import { LinkButton } from "@cloudflare/kumo";
import { GithubLogoIcon } from "@phosphor-icons/react";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { meQuery, setupQuery } from "../api/auth";
import {
  AuthCard,
  authButtonClass,
  authErrorClass,
  authFormClass,
} from "../features/auth/AuthCard";
import { safeNext } from "../lib/next";

/** next is a relative path to return to after signing in. */
type LoginSearch = { error?: string; next?: string };

export const Route = createFileRoute("/login")({
  validateSearch: (search): LoginSearch => ({
    ...(typeof search.error === "string" ? { error: search.error } : {}),
    ...(safeNext(search.next) ? { next: safeNext(search.next) } : {}),
  }),
  beforeLoad: async ({ context: { queryClient }, search }) => {
    const setup = await queryClient.fetchQuery(setupQuery);
    if (!setup.githubConfigured) throw redirect({ to: "/setup" });
    const me = await queryClient.fetchQuery(meQuery).catch(() => null);
    // next may be a server route such as /oauth/authorize, so leave the SPA for it.
    if (me) {
      throw search.next
        ? redirect({ href: search.next, reloadDocument: true })
        : redirect({ to: "/" });
    }
  },
  component: LoginPage,
});

function LoginPage() {
  const { error, next } = Route.useSearch();
  return (
    <AuthCard title="Sign in to shed" description="Use the GitHub account that owns this server.">
      <div className={authFormClass}>
        {error && (
          <p className={authErrorClass} role="alert">
            {error === "denied"
              ? "That GitHub account isn't allowed to sign in here."
              : `Sign in failed: ${error}`}
          </p>
        )}
        <LinkButton
          href={next ? `/api/auth/login?next=${encodeURIComponent(next)}` : "/api/auth/login"}
          variant="primary"
          size="lg"
          className={authButtonClass}
          icon={<GithubLogoIcon size={18} />}
        >
          Sign in with GitHub
        </LinkButton>
      </div>
    </AuthCard>
  );
}
