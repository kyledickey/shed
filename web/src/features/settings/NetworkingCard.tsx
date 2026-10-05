import { ArrowUpRight, Ellipsis, Globe, Lock, Plus, Sparkles, Trash2 } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useAddDomain, useDeleteDomain } from "../../api/services";
import type { Domain, Service } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input } from "../../components/Form";
import { CopyButton, ServiceIcon } from "../../components/Misc";
import { Dialog, Menu, MenuItem, MenuSeparator, useToast } from "../../components/Overlay";
import { ConfirmDialog } from "./ConfirmDialog";
import { SettingsCard, useSectionForm } from "./SectionForm";
import styles from "./Settings.module.css";
import { HelpTip } from "../docs/HelpTip";

/** NetworkingCard shows the private address, public domains, and the public TCP port. */
export function NetworkingCard({ service }: { service: Service }) {
  return (
    <div className={styles.stack}>
      <LayerCard
        title={
          <>
            Networking <HelpTip topic="privateNetwork" />
          </>
        }
        meta="How traffic reaches this service."
      >
        <div className={styles.list}>
          <div className={styles.row}>
            <ServiceIcon icon={<Lock />} tone="neutral" size={28} />
            <div className={styles.rowMain}>
              <span className={styles.mono}>
                {service.privateHost}
                {service.port > 0 && `:${service.port}`}
              </span>
              <span className={styles.muted}>
                Private network. Other services in this project reach it by this name.
              </span>
            </div>
            <CopyButton
              value={
                service.port > 0 ? `${service.privateHost}:${service.port}` : service.privateHost
              }
              label="Copy private address"
            />
          </div>
        </div>
      </LayerCard>
      {service.kind === "app" && <DomainsCard service={service} />}
      <PublicPortCard service={service} />
    </div>
  );
}

function DomainsCard({ service }: { service: Service }) {
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Domain | null>(null);
  const remove = useDeleteDomain(service.id);
  const toast = useToast();

  return (
    <LayerCard
      title={
        <>
          Public domains <HelpTip topic="domains" />
        </>
      }
      meta="Certificates are issued automatically."
      actions={
        <Button size="sm" onClick={() => setAdding(true)}>
          <Plus size={13} /> Add domain
        </Button>
      }
      footer={
        <span className={styles.muted}>
          Point custom domains at this server with an A or AAAA record.
        </span>
      }
    >
      {service.domains.length === 0 ? (
        <div className={styles.section}>
          <p className={styles.note}>No public domains. The service is only reachable privately.</p>
        </div>
      ) : (
        <div className={styles.list}>
          {service.domains.map((d) => (
            <div key={d.id} className={styles.row}>
              <ServiceIcon icon={<Globe />} tone="sky" size={28} />
              <a className={styles.host} href={d.url} target="_blank" rel="noreferrer">
                {d.host}
                <ArrowUpRight size={12} />
              </a>
              {d.generated && (
                <Badge size="sm" tone="neutral">
                  generated
                </Badge>
              )}
              <span className={styles.spacer} />
              <Menu
                trigger={
                  <Button variant="ghost" size="sm" icon aria-label={`Actions for ${d.host}`}>
                    <Ellipsis size={14} />
                  </Button>
                }
              >
                <MenuItem
                  icon={<ArrowUpRight size={14} />}
                  onClick={() => window.open(d.url, "_blank", "noreferrer")}
                >
                  Open
                </MenuItem>
                <MenuSeparator />
                <MenuItem danger icon={<Trash2 size={14} />} onClick={() => setRemoving(d)}>
                  Remove
                </MenuItem>
              </Menu>
            </div>
          ))}
        </div>
      )}
      <AddDomainDialog service={service} open={adding} onOpenChange={setAdding} />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(open) => {
          if (!open) setRemoving(null);
          remove.reset();
        }}
        title="Remove domain"
        confirmLabel="Remove"
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.id, {
            onSuccess: () => {
              toast.add({ type: "success", title: "Domain removed" });
              setRemoving(null);
            },
          })
        }
      >
        <strong>{removing?.host}</strong> will stop routing to this service.
      </ConfirmDialog>
    </LayerCard>
  );
}

function AddDomainDialog({
  service,
  open,
  onOpenChange,
}: {
  service: Service;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const add = useAddDomain(service.id);
  const toast = useToast();
  const [host, setHost] = useState("");

  const submit = (value?: string) =>
    add.mutate(value, {
      onSuccess: (d) => {
        toast.add({ type: "success", title: "Domain added", description: d.host });
        setHost("");
        onOpenChange(false);
      },
    });

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) add.reset();
        onOpenChange(next);
      }}
      title="Add domain"
      description={
        <>
          Point an A or AAAA record for the domain at this server, or generate one under the base
          domain. <HelpTip topic="generatedDomains" />
        </>
      }
      footer={
        <>
          <Button onClick={() => submit()} disabled={add.isPending}>
            <Sparkles size={14} /> Generate domain
          </Button>
          <Button
            variant="primary"
            loading={add.isPending}
            disabled={!host.trim()}
            onClick={() => submit(host.trim().toLowerCase())}
          >
            Add domain
          </Button>
        </>
      }
    >
      <form
        className={styles.fields}
        onSubmit={(e) => {
          e.preventDefault();
          if (host.trim()) submit(host.trim().toLowerCase());
        }}
      >
        <Field label="Domain" error={add.error ? errorMessage(add.error) : undefined}>
          {(id) => (
            <Input
              id={id}
              mono
              autoFocus
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="app.example.com"
            />
          )}
        </Field>
      </form>
    </Dialog>
  );
}

function PublicPortCard({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({ publicPort: s.publicPort }));
  return (
    <SettingsCard
      title={
        <>
          Public TCP port <HelpTip topic="publicPort" />
        </>
      }
      meta="Publish the service on a host port."
      form={form}
    >
      <Field
        label="Host port"
        hint="Reach the service from outside, e.g. a database. 0 keeps it private."
      >
        {(id) => (
          <Input
            id={id}
            type="number"
            min={0}
            max={65535}
            mono
            value={form.draft.publicPort}
            onChange={(e) => form.set("publicPort", Number(e.target.value) || 0)}
          />
        )}
      </Field>
    </SettingsCard>
  );
}
