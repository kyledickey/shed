/** Endpoint is one row of the API reference. */
export type Endpoint = {
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  /** Request body type, if any. */
  body?: string;
  /** Response type or status. */
  response: string;
  notes?: string;
};

/** EndpointGroup is a titled set of endpoints. */
export type EndpointGroup = { title: string; endpoints: Endpoint[] };

/** endpointGroups is the complete HTTP API, as listed in docs/design.md. */
export const endpointGroups: EndpointGroup[] = [
  {
    title: "Session and setup",
    endpoints: [
      { method: "GET", path: "/api/me", response: "User" },
      {
        method: "GET",
        path: "/api/auth/login",
        response: "302 to GitHub",
        notes: "Public. Sets the state cookie.",
      },
      {
        method: "GET",
        path: "/api/auth/callback",
        response: "302 to /",
        notes: "Public. Sets shed_session.",
      },
      {
        method: "POST",
        path: "/api/auth/logout",
        response: "204",
        notes: "Public route, origin and JSON checks apply.",
      },
      { method: "GET", path: "/api/setup", response: "Setup", notes: "Public." },
      {
        method: "GET",
        path: "/api/setup/github?token=",
        response: "HTML form",
        notes: "Public. Auto-submits the app manifest.",
      },
      {
        method: "GET",
        path: "/api/setup/github/callback?code=&state=",
        response: "302 to GitHub",
        notes: "Public.",
      },
      {
        method: "POST",
        path: "/api/setup/github/import",
        body: "ImportApp",
        response: "Setup",
        notes: "Public, setup token required.",
      },
    ],
  },
  {
    title: "Projects and services",
    endpoints: [
      { method: "GET", path: "/api/projects", response: "Project[]" },
      { method: "POST", path: "/api/projects", body: "{name}", response: "Project" },
      { method: "GET", path: "/api/projects/{id}", response: "ProjectDetail" },
      { method: "PATCH", path: "/api/projects/{id}", body: "{name}", response: "Project" },
      {
        method: "DELETE",
        path: "/api/projects/{id}",
        response: "204",
        notes: "Tears down everything.",
      },
      {
        method: "POST",
        path: "/api/projects/{id}/services",
        body: "NewService",
        response: "Service",
        notes: "Creates the service and runs its first deploy.",
      },
      { method: "GET", path: "/api/services/{id}", response: "Service" },
      { method: "PATCH", path: "/api/services/{id}", body: "ServicePatch", response: "Service" },
      {
        method: "DELETE",
        path: "/api/services/{id}",
        response: "204",
        notes: "Containers, volumes, and images.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/stop",
        response: "Service",
        notes: "Cancels deploys, stops the container, removes routes.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/start",
        response: "Service",
        notes: "409 if never deployed or fenced.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/restart",
        response: "Service",
        notes: "409 if stopped, never deployed, or fenced.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/restore-fence/clear",
        response: "Service",
        notes: "Keeps the current data. 409 while held.",
      },
      { method: "GET", path: "/api/services/{id}/variables", response: "Record<string,string>" },
      {
        method: "PUT",
        path: "/api/services/{id}/variables",
        body: "Record<string,string>",
        response: "Record<string,string>",
        notes: "Replaces all variables.",
      },
      {
        method: "GET",
        path: "/api/services/{id}/variables/resolved",
        response: "Record<string,string>",
        notes: "References expanded, injected variables included.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/domains",
        body: "{host?}",
        response: "Domain",
        notes: "No host generates one.",
      },
      { method: "DELETE", path: "/api/domains/{id}", response: "204" },
      {
        method: "POST",
        path: "/api/services/{id}/volumes",
        body: "{mountPath}",
        response: "Volume",
      },
      { method: "DELETE", path: "/api/volumes/{id}", response: "204", notes: "Removes the data." },
    ],
  },
  {
    title: "Deployments, logs, and metrics",
    endpoints: [
      {
        method: "GET",
        path: "/api/services/{id}/deployments",
        response: "Deployment[]",
        notes: "Newest first, 50.",
      },
      {
        method: "POST",
        path: "/api/services/{id}/deployments",
        response: "Deployment",
        notes: "Deploys the branch head or image. 409 if fenced.",
      },
      { method: "GET", path: "/api/deployments/{id}", response: "Deployment" },
      {
        method: "POST",
        path: "/api/deployments/{id}/redeploy",
        response: "Deployment",
        notes: "Reuses the image, so it is a rollback. 409 if fenced.",
      },
      { method: "POST", path: "/api/deployments/{id}/cancel", response: "Deployment" },
      {
        method: "GET",
        path: "/api/deployments/{id}/logs",
        response: "SSE",
        notes: "Replays the build log, follows while building.",
      },
      {
        method: "GET",
        path: "/api/services/{id}/logs",
        response: "SSE",
        notes: "Runtime logs: last 500 lines, then follow.",
      },
      {
        method: "GET",
        path: "/api/services/{id}/metrics?range=",
        response: "Metrics",
        notes: "range is 1h, 6h, 24h, or 7d. Default 1h.",
      },
      {
        method: "GET",
        path: "/api/host/metrics?range=",
        response: "HostMetrics",
        notes: "Same ranges.",
      },
      {
        method: "GET",
        path: "/api/logs",
        response: "SSE",
        notes: "shed's own log: last 1000 lines, then follow.",
      },
    ],
  },
  {
    title: "Backups",
    endpoints: [
      {
        method: "GET",
        path: "/api/services/{id}/backups",
        response: "ServiceBackups",
        notes: "Newest first, 100.",
      },
      {
        method: "PUT",
        path: "/api/services/{id}/backups/policy",
        body: "BackupPolicyInput",
        response: "BackupPolicy",
      },
      {
        method: "POST",
        path: "/api/services/{id}/backups",
        response: "202, Backup",
        notes:
          "Run now. 409 if one is queued, running, or uploading. 400 without volumes or a deployment.",
      },
      { method: "GET", path: "/api/backups/system", response: "SystemBackups" },
      {
        method: "PUT",
        path: "/api/backups/system/policy",
        body: "BackupPolicyInput",
        response: "BackupPolicy",
      },
      { method: "POST", path: "/api/backups/system", response: "202, Backup" },
      {
        method: "GET",
        path: "/api/backups/{id}/download",
        response: "archive",
        notes: "Decrypted, still zstd-compressed. Content-Disposition names it.",
      },
      {
        method: "POST",
        path: "/api/backups/{id}/restore",
        response: "202, Restore",
        notes: "409 if busy or fenced. 400 for shed.db, unsuccessful, or vanished backups.",
      },
      {
        method: "DELETE",
        path: "/api/backups/{id}",
        response: "204",
        notes: "Local file and S3 object. 409 while active or being restored.",
      },
      {
        method: "GET",
        path: "/api/backups/settings",
        response: "BackupSettings",
        notes: "Never includes the S3 secret.",
      },
      {
        method: "PUT",
        path: "/api/backups/settings",
        body: "BackupSettingsInput",
        response: "BackupSettings",
      },
      {
        method: "POST",
        path: "/api/backups/settings/test",
        body: "BackupSettingsInput",
        response: "204",
        notes: "400 with the S3 error. A blank secret uses the stored one.",
      },
      {
        method: "GET",
        path: "/api/backups/settings/key",
        response: "{ identity }",
        notes: "The age secret key. 404 if none.",
      },
    ],
  },
  {
    title: "GitHub",
    endpoints: [
      { method: "GET", path: "/api/github/repos", response: "Repo[]" },
      { method: "GET", path: "/api/github/repos/{owner}/{repo}/branches", response: "string[]" },
      {
        method: "POST",
        path: "/api/github/webhook",
        response: "202",
        notes: "Public. HMAC-signed by GitHub.",
      },
    ],
  },
];

/**
 * curlCommand is the shell command that calls an endpoint, with a placeholder
 * for the session cookie. Path parameters become <id> placeholders, and
 * mutations carry the JSON content type shed requires.
 */
export function curlCommand(origin: string, e: Pick<Endpoint, "method" | "path">): string {
  const path = e.path.replace(/\{[^}]+\}/g, "<id>");
  const lines = [`curl -s${e.method === "GET" ? "" : ` -X ${e.method}`} '${origin}${path}'`];
  lines.push("  -H 'Cookie: shed_session=<your session token>'");
  if (e.method !== "GET" && e.method !== "DELETE") {
    lines.push("  -H 'Content-Type: application/json'", "  -d '{}'");
  }
  return lines.join(" \\\n");
}
