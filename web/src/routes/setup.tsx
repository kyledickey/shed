import { createFileRoute, redirect } from "@tanstack/react-router";
import { useState } from "react";
import { setupQuery } from "../api/auth";
import { Button } from "../components/Button";
import { Field } from "../components/Field";
import { GitHubIcon } from "../components/GitHubIcon";
import { Input } from "../components/Input";
import { AuthLayout, authFormClass } from "../features/auth/AuthLayout";

export const Route = createFileRoute("/setup")({
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery);
    if (setup.githubConfigured) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

function SetupPage() {
  const [token, setToken] = useState("");
  return (
    <AuthLayout
      title="Create the GitHub App"
      description="shed uses a private GitHub App for sign-in, repository access, and push-to-deploy. GitHub will ask you to confirm it, then to pick the repositories it can access."
    >
      <form method="get" action="/api/setup/github" className={authFormClass}>
        <Field
          label="Setup token"
          hint={
            <>
              Printed once in the server log (<code>shed.log</code> or{" "}
              <code>journalctl -u shed</code>).
            </>
          }
        >
          <Input
            name="token"
            mono
            required
            autoFocus
            value={token}
            onChange={(e) => setToken(e.target.value.trim())}
          />
        </Field>
        <Button type="submit" variant="primary" disabled={!token}>
          <GitHubIcon size={14} />
          Create GitHub App
        </Button>
      </form>
    </AuthLayout>
  );
}
