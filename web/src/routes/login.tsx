import { createFileRoute, redirect } from "@tanstack/react-router";
import { meQuery, setupQuery } from "../api/auth";
import { Banner } from "../components/Banner";
import { LinkButton } from "../components/Button";
import { GitHubIcon } from "../components/GitHubIcon";
import { AuthLayout } from "../features/auth/AuthLayout";

type LoginSearch = { error?: string };

export const Route = createFileRoute("/login")({
  validateSearch: (search): LoginSearch =>
    typeof search.error === "string" ? { error: search.error } : {},
  beforeLoad: async ({ context: { queryClient } }) => {
    const setup = await queryClient.fetchQuery(setupQuery);
    if (!setup.githubConfigured) throw redirect({ to: "/setup" });
    const me = await queryClient.fetchQuery(meQuery).catch(() => null);
    if (me) throw redirect({ to: "/" });
  },
  component: LoginPage,
});

function LoginPage() {
  const { error } = Route.useSearch();
  return (
    <AuthLayout title="Sign in" description="Use the GitHub account that owns this shed.">
      {error && (
        <Banner tone="danger">
          {error === "denied"
            ? "That GitHub account isn't allowed to sign in here."
            : `Sign in failed: ${error}`}
        </Banner>
      )}
      <LinkButton href="/api/auth/login" variant="primary">
        <GitHubIcon size={14} />
        Sign in with GitHub
      </LinkButton>
    </AuthLayout>
  );
}
