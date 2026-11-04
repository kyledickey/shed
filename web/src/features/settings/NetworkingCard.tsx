import { Button, ClipboardText, DropdownMenu, Input, Table } from "@cloudflare/kumo";
import {
  ArrowUpRightIcon,
  DotsThreeIcon,
  PlusIcon,
  SparkleIcon,
  TrashIcon,
} from "@phosphor-icons/react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useAddDomain, useDeleteDomain } from "../../api/services";
import type { Domain, Service } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { Pill } from "../../components/Pill";
import { SettingRow, Well } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { HelpTip } from "../docs/HelpTip";
import { ConfirmDialog } from "./ConfirmDialog";
import { NumberInput } from "./fields";
import styles from "./Form.module.css";
import { FormDialog } from "./FormDialog";
import { SettingsForm, useSectionForm } from "./SectionForm";

/** NetworkingSection shows the private address, public domains, and the public TCP port. */
export function NetworkingSection({ service }: { service: Service }) {
  const address = service.port > 0 ? `${service.privateHost}:${service.port}` : service.privateHost;
  return (
    <>
      <SettingRow
        label={
          <>
            <HelpTip topic="privateNetwork">Private address</HelpTip>
          </>
        }
        description="Other services in this project reach it by this name."
        control={<ClipboardText text={address} size="base" className={styles.mono} />}
      />
      {service.kind === "app" && <Domains service={service} />}
      <PublicPort service={service} />
    </>
  );
}

function Domains({ service }: { service: Service }) {
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Domain | null>(null);
  const remove = useDeleteDomain(service.id);
  const toast = useToast();

  return (
    <SettingRow
      label={
        <>
          <HelpTip topic="domains">Public domains</HelpTip>
        </>
      }
      description="Certificates are issued automatically. Point custom domains at this server with an A or AAAA record."
      control={
        <Button icon={<PlusIcon />} onClick={() => setAdding(true)}>
          Add domain
        </Button>
      }
    >
      {service.domains.length === 0 ? (
        <Well empty>No public domains. The service is only reachable privately.</Well>
      ) : (
        <div className={styles.table}>
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.Head>Domain</Table.Head>
                <Table.Head>Type</Table.Head>
                <Table.Head aria-label="Actions" />
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {service.domains.map((d) => (
                <Table.Row key={d.id}>
                  <Table.Cell>
                    <a className={styles.host} href={d.url} target="_blank" rel="noreferrer">
                      {d.host}
                      <ArrowUpRightIcon size={12} />
                    </a>
                  </Table.Cell>
                  <Table.Cell>
                    {d.generated ? (
                      <Pill>Generated</Pill>
                    ) : (
                      <span className={styles.muted}>Custom</span>
                    )}
                  </Table.Cell>
                  <Table.Cell className={styles.cellActions}>
                    <DropdownMenu>
                      <DropdownMenu.Trigger
                        render={
                          <Button
                            variant="ghost"
                            size="sm"
                            shape="square"
                            icon={<DotsThreeIcon weight="bold" />}
                            aria-label={`Actions for ${d.host}`}
                          />
                        }
                      />
                      <DropdownMenu.Content align="end">
                        <DropdownMenu.Item
                          icon={<ArrowUpRightIcon />}
                          onClick={() => window.open(d.url, "_blank", "noreferrer")}
                        >
                          Open
                        </DropdownMenu.Item>
                        <DropdownMenu.Item
                          variant="danger"
                          icon={<TrashIcon />}
                          onClick={() => setRemoving(d)}
                        >
                          Remove
                        </DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
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
    </SettingRow>
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
    <FormDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) add.reset();
        onOpenChange(next);
      }}
      title="Add domain"
      description={
        <>
          Point an A or AAAA record for the domain at this server, or generate one under the{" "}
          <HelpTip topic="generatedDomains">base domain</HelpTip>.
        </>
      }
      footer={
        <>
          <Button icon={<SparkleIcon />} onClick={() => submit()} disabled={add.isPending}>
            Generate domain
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
        onSubmit={(e) => {
          e.preventDefault();
          if (host.trim()) submit(host.trim().toLowerCase());
        }}
      >
        <Input
          label="Domain"
          className={cx(styles.mono)}
          autoFocus
          value={host}
          onChange={(e) => setHost(e.target.value)}
          placeholder="app.example.com"
          spellCheck={false}
          error={add.error ? errorMessage(add.error) : undefined}
        />
      </form>
    </FormDialog>
  );
}

function PublicPort({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({ publicPort: s.publicPort }));
  return (
    <SettingsForm form={form}>
      <SettingRow
        label={
          <>
            <HelpTip topic="publicPort">Public TCP port</HelpTip>
          </>
        }
        description="Publish the service on a host port to reach it from outside, e.g. a database. 0 keeps it private."
        control={
          <NumberInput
            aria-label="Public TCP port"
            min={0}
            max={65535}
            value={form.draft.publicPort}
            onChange={(e) => form.set("publicPort", Number(e.target.value) || 0)}
          />
        }
      />
    </SettingsForm>
  );
}
