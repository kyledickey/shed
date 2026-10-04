import { createFileRoute, redirect, useNavigate } from "@tanstack/react-router";
import { CircleX, FileKey, Info } from "lucide-react";
import { useState, type FormEvent } from "react";
import { setupQuery, useImportApp } from "../api/auth";
import { errorMessage } from "../api/client";
import { Button, buttonClass } from "../components/Button";
import { Field, Input, Segmented, Textarea } from "../components/Form";
import { Callout, GitHubIcon } from "../components/Misc";
import { AuthCard, authFormClass } from "../features/auth/AuthCard";

type SetupSearch = { error?: string };

type Mode = "create" | "existing";

const errors: Record<string, string> = {
  invalid_token: "That setup token isn't valid. Copy it again from the server log.",
  failed: "GitHub App setup didn't finish. Check the server log, then try again.",
};

export const Route = createFileRoute("/setup")({
  validateSearch: (search): SetupSearch =>
    typeof search.error === "string" ? { error: search.error } : {},
  beforeLoad: async ({ context }) => {
    const setup = await context.queryClient.fetchQuery(setupQuery);
    if (setup.githubConfigured) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

function SetupPage() {
  const { error } = Route.useSearch();
  const [mode, setMode] = useState<Mode>("create");
  const [token, setToken] = useState("");
  return (
    <AuthCard
      title={mode === "create" ? "Create the GitHub App" : "Connect a GitHub App"}
      description={
        mode === "create"
          ? "shed uses a GitHub App for sign-in, repository access, and push-to-deploy. GitHub will ask you to confirm it, then to pick the repositories it can access."
          : "Reuse a GitHub App that shed created before. GitHub shows its secrets only once, so generate new ones in the app's settings."
      }
    >
      <Segmented
        label="GitHub App"
        value={mode}
        onChange={setMode}
        options={[
          { value: "create", label: "New app" },
          { value: "existing", label: "Existing app" },
        ]}
      />
      {error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {errors[error] ?? `Setup failed: ${error}`}
        </Callout>
      )}
      {mode === "create" ? (
        <form method="get" action="/api/setup/github" className={authFormClass}>
          <TokenField value={token} onChange={setToken} />
          <Button type="submit" variant="primary" size="lg" disabled={!token}>
            <GitHubIcon size={16} />
            Create GitHub App
          </Button>
        </form>
      ) : (
        <ExistingAppForm token={token} setToken={setToken} />
      )}
    </AuthCard>
  );
}

function TokenField({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <Field
      label="Setup token"
      hint={
        <>
          Printed once in the server log (<code>shed.log</code> or <code>journalctl -u shed</code>).
        </>
      }
    >
      {(id) => (
        <Input
          id={id}
          name="token"
          mono
          required
          autoFocus
          autoComplete="off"
          value={value}
          onChange={(e) => onChange(e.target.value.trim())}
        />
      )}
    </Field>
  );
}

function ExistingAppForm({ token, setToken }: { token: string; setToken: (v: string) => void }) {
  const navigate = useNavigate();
  const importApp = useImportApp();
  const [appId, setAppId] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [privateKey, setPrivateKey] = useState("");
  const origin = window.location.origin;
  const complete = token && appId && clientId && clientSecret && webhookSecret && privateKey;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    importApp.mutate(
      { token, appId: Number(appId), clientId, clientSecret, webhookSecret, privateKey },
      { onSuccess: () => navigate({ to: "/login" }) },
    );
  };

  const text = (label: string, value: string, set: (v: string) => void, hint?: string) => (
    <Field label={label} hint={hint}>
      {(id) => (
        <Input
          id={id}
          mono
          required
          autoComplete="off"
          value={value}
          onChange={(e) => set(e.target.value.trim())}
        />
      )}
    </Field>
  );

  return (
    <form onSubmit={submit} className={authFormClass}>
      <Callout tone="sky" icon={<Info size={16} />}>
        In the app's settings on GitHub, generate a client secret and a private key, and set a
        webhook secret. Its webhook URL must be <code>{origin}/api/github/webhook</code> and its
        callback URL <code>{origin}/api/auth/callback</code>.
      </Callout>
      <TokenField value={token} onChange={setToken} />
      <Field label="App ID">
        {(id) => (
          <Input
            id={id}
            mono
            required
            inputMode="numeric"
            autoComplete="off"
            value={appId}
            onChange={(e) => setAppId(e.target.value.replace(/\D/g, ""))}
          />
        )}
      </Field>
      {text("Client ID", clientId, setClientId)}
      {text("Client secret", clientSecret, setClientSecret)}
      {text("Webhook secret", webhookSecret, setWebhookSecret)}
      <Field
        label="Private key"
        hint="Paste the contents of the .pem file GitHub downloaded, or choose the file."
      >
        {(id) => (
          <>
            <Textarea
              id={id}
              mono
              required
              spellCheck={false}
              placeholder="-----BEGIN RSA PRIVATE KEY-----"
              value={privateKey}
              onChange={(e) => setPrivateKey(e.target.value)}
            />
            <label className={buttonClass({ size: "sm" })}>
              <input
                type="file"
                accept=".pem"
                hidden
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (file) setPrivateKey(await file.text());
                }}
              />
              <FileKey size={14} />
              Choose .pem file
            </label>
          </>
        )}
      </Field>
      {importApp.error && (
        <Callout tone="tomato" icon={<CircleX size={16} />}>
          {errorMessage(importApp.error)}
        </Callout>
      )}
      <Button
        type="submit"
        variant="primary"
        size="lg"
        disabled={!complete}
        loading={importApp.isPending}
      >
        <GitHubIcon size={16} />
        Connect GitHub App
      </Button>
    </form>
  );
}
