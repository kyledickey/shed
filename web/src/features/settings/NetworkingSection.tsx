import { ArrowUpRight, Plus, Sparkles, Trash } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useAddDomain, useDeleteDomain } from "../../api/services";
import type { Domain, Service } from "../../api/types";
import { ErrorText } from "../../components/Banner";
import { Button } from "../../components/Button";
import { Card } from "../../components/Card";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { CopyButton } from "../../components/CopyButton";
import { Input } from "../../components/Input";
import { useSectionForm } from "./SettingsSection";
import styles from "./Settings.module.css";

export function NetworkingSection({ service }: { service: Service }) {
  return (
    <Card title="Networking">
      <Subsection
        title="Private network"
        hint="Other services in this project connect using this hostname."
      >
        <div className={styles.item}>
          <code className={styles.mono}>
            {service.privateHost}
            {service.port > 0 && <span className={styles.muted}>:{service.port}</span>}
          </code>
          <CopyButton value={service.privateHost} label="Copy hostname" />
        </div>
      </Subsection>
      {service.kind === "app" && <Domains service={service} />}
      <PublicPort service={service} />
    </Card>
  );
}

function Subsection({
  title,
  hint,
  children,
}: {
  title: string;
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className={styles.subsection}>
      <div>
        <h3 className={styles.subtitle}>{title}</h3>
        {hint && <p className={styles.hint}>{hint}</p>}
      </div>
      {children}
    </div>
  );
}

function Domains({ service }: { service: Service }) {
  const add = useAddDomain(service.id);
  const [host, setHost] = useState("");
  const [removing, setRemoving] = useState<Domain | null>(null);
  const remove = useDeleteDomain(service.id);

  const submit = (value?: string) => add.mutate(value, { onSuccess: () => setHost("") });

  return (
    <Subsection
      title="Public domains"
      hint={
        service.port > 0
          ? "HTTPS is provisioned automatically. Point custom domains' DNS at this server."
          : "Set a port in the Deploy section so traffic can reach this service."
      }
    >
      {service.domains.length > 0 && (
        <ul className={styles.list}>
          {service.domains.map((d) => (
            <li key={d.id} className={styles.item}>
              <a className={styles.link} href={d.url} target="_blank" rel="noreferrer">
                {d.host}
                <ArrowUpRight size={12} />
              </a>
              {d.generated && <span className={styles.badge}>Generated</span>}
              <Button
                variant="ghost"
                size="sm"
                icon
                aria-label={`Remove ${d.host}`}
                onClick={() => setRemoving(d)}
              >
                <Trash size={14} />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form
        className={styles.inline}
        onSubmit={(e) => {
          e.preventDefault();
          if (host.trim()) submit(host.trim().toLowerCase());
        }}
      >
        <Input
          mono
          value={host}
          onChange={(e) => setHost(e.target.value)}
          placeholder="app.example.com"
          aria-label="Custom domain"
        />
        <Button type="submit" disabled={!host.trim() || add.isPending}>
          <Plus size={14} />
          Add
        </Button>
        <Button onClick={() => submit()} disabled={add.isPending}>
          <Sparkles size={14} />
          Generate
        </Button>
      </form>
      <ErrorText error={add.error} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
          remove.reset();
        }}
        title="Remove domain"
        confirmLabel="Remove"
        pending={remove.isPending}
        error={remove.error?.message}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.id, {
            onSuccess: () => setRemoving(null),
          })
        }
      >
        <p>
          <strong>{removing?.host}</strong> will stop routing to this service.
        </p>
      </ConfirmDialog>
    </Subsection>
  );
}

function PublicPort({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({ publicPort: s.publicPort }));
  return (
    <Subsection
      title="Public TCP port"
      hint="Publish the service's port on this host port, e.g. to reach a database from outside. Leave empty to keep it private."
    >
      <form
        className={styles.inline}
        onSubmit={(e) => {
          e.preventDefault();
          if (form.dirty) form.save();
        }}
      >
        <Input
          type="number"
          min={0}
          max={65535}
          mono
          value={form.draft.publicPort || ""}
          onChange={(e) => form.set("publicPort", Number(e.target.value) || 0)}
          placeholder="None"
          aria-label="Public TCP port"
        />
        <Button type="submit" disabled={!form.dirty} loading={form.pending}>
          Save
        </Button>
      </form>
      <ErrorText error={form.error} />
    </Subsection>
  );
}
