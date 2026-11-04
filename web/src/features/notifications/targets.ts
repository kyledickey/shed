/** Field is one input in a channel's target form. */
export type Field = {
  key: string;
  label: string;
  description?: string;
  placeholder?: string;
  secret?: boolean;
  optional?: boolean;
  /** options makes the field a select; it starts on the first option. */
  options?: { value: string; label: string }[];
};

/** TargetForm is how a provider's target is entered: its fields, and how they become the target string. */
export type TargetForm = {
  fields: Field[];
  compose(values: Record<string, string>): string;
};

const fallback: TargetForm = {
  fields: [{ key: "target", label: "Target", secret: true }],
  compose: (v) => field(v, "target"),
};

/** targetForms holds the target form of each known provider. */
export const targetForms: Record<string, TargetForm> = {
  hark: {
    fields: [
      {
        key: "url",
        label: "Webhook URL",
        description:
          "The webhook URL from the Hark app, like https://hark.ryan.ceo/hooks/<token>. It works like a password.",
        placeholder: "https://hark.ryan.ceo/hooks/…",
        secret: true,
      },
    ],
    compose: (v) => field(v, "url"),
  },
  ntfy: {
    fields: [
      {
        key: "url",
        label: "Topic URL",
        description: "The topic on ntfy.sh or your own ntfy server.",
        placeholder: "https://ntfy.sh/my-topic",
      },
      {
        key: "token",
        label: "Access token",
        description: "Needed when the topic is protected.",
        placeholder: "tk_…",
        secret: true,
        optional: true,
      },
    ],
    compose: (v) => {
      const raw = field(v, "url");
      const token = field(v, "token");
      if (!token) return raw;
      let u: URL;
      try {
        u = new URL(raw);
      } catch {
        throw new Error("Topic URL is not a valid URL.");
      }
      u.username = "";
      u.password = token;
      return u.href;
    },
  },
  smtp: {
    fields: [
      { key: "host", label: "Server", placeholder: "smtp.example.com" },
      {
        key: "security",
        label: "Security",
        options: [
          { value: "smtp", label: "STARTTLS" },
          { value: "smtps", label: "TLS" },
        ],
      },
      {
        key: "port",
        label: "Port",
        description: "Defaults to 587 for STARTTLS and 465 for TLS.",
        optional: true,
      },
      { key: "username", label: "Username", optional: true },
      { key: "password", label: "Password", secret: true, optional: true },
      { key: "from", label: "From", placeholder: "shed@example.com" },
      {
        key: "to",
        label: "To",
        description: "One or more addresses, separated by commas.",
        placeholder: "you@example.com",
      },
    ],
    compose: (v) => {
      const user = field(v, "username");
      const pass = v.password ?? "";
      const userinfo =
        user || pass
          ? `${encodeURIComponent(user)}${pass ? `:${encodeURIComponent(pass)}` : ""}@`
          : "";
      const port = field(v, "port") ? `:${field(v, "port")}` : "";
      const query = new URLSearchParams({ from: field(v, "from"), to: field(v, "to") });
      const host = field(v, "host");
      const bracketed = host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
      return `${field(v, "security") || "smtp"}://${userinfo}${bracketed}${port}?${query}`;
    },
  },
};

/** targetForm returns the provider's target form, or a single target field for one the dashboard doesn't know. */
export function targetForm(provider: string): TargetForm {
  return targetForms[provider] ?? fallback;
}

function field(values: Record<string, string>, key: string): string {
  return values[key]?.trim() ?? "";
}
