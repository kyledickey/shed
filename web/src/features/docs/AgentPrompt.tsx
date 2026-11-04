import { Button } from "@cloudflare/kumo";
import { useEffect, useState } from "react";

/** agentInstallPrompt asks a coding agent with SSH access to install shed. */
export const agentInstallPrompt = `Install shed (https://shed.land), a self-hosted deployment platform, on my Linux server.

1. Connect to the server over SSH. Ask me for the host and user if you don't have them. It needs Linux (amd64 or arm64) with systemd, root or sudo, and ports 80 and 443 reachable from the internet.
2. Ask me for:
   - the dashboard domain, such as shed.example.com
   - the GitHub logins allowed to sign in
   - optionally, an apps base domain for generated domains (needs a wildcard DNS record *.<base>)
   - optionally, an email for Let's Encrypt
3. Check that the dashboard domain has an A or AAAA record pointing at the server's public IP. If it doesn't, tell me which record to create and wait for me.
4. Run the installer unattended, leaving out the optional variables I didn't give you:
   curl -fsSL https://shed.land/install.sh | sudo SHED_DOMAIN=<domain> SHED_ALLOWED_USERS=<logins> SHED_BASE_DOMAIN=<base> SHED_ACME_EMAIL=<email> SHED_YES=1 bash
5. When it finishes, give me the setup URL and the one-time setup token it printed, and remind me to back up /etc/shed/shed.key somewhere off the server.

Don't change the firewall, other services, or existing config without asking me first. If something fails, show me the output and the last 100 lines of \`journalctl -u shed\`.

Installation docs: https://shed.land/docs/installation`;

/** AgentPrompt copies a prompt that has a coding agent install shed over SSH. */
export function AgentPrompt({ size }: { size?: "sm" | "base" | "lg" }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <Button
      variant="secondary"
      size={size}
      onClick={() =>
        void navigator.clipboard.writeText(agentInstallPrompt).then(() => setCopied(true))
      }
    >
      {copied ? "Copied" : "Copy agent prompt"}
    </Button>
  );
}
