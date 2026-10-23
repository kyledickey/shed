import { Link } from "@tanstack/react-router";
import { PlugZap } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useUpdateService } from "../../api/services";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { Callout, OnePasswordIcon } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import settings from "../settings/Settings.module.css";
import { useRedeployHint } from "../services/redeploy";
import styles from "./Variables.module.css";

/** OnePasswordLink is a button that opens a dialog to link or unlink a 1Password Environment. */
export function OnePasswordLink({ service, connected }: { service: Service; connected: boolean }) {
  const linked = service.onePasswordEnvironment;
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState("");
  const update = useUpdateService(service.id);
  const hint = useRedeployHint();
  const toast = useToast();
  const id = draft.trim();
  const canLink = connected && id !== "" && id !== linked;

  const setEnvironment = (environment: string) =>
    update.mutate(
      { onePasswordEnvironment: environment },
      {
        onSuccess: () => {
          setOpen(false);
          hint.markPending();
          toast.add({
            type: "success",
            title: environment ? "Environment linked" : "Environment unlinked",
            description: "Redeploy to apply.",
          });
        },
      },
    );

  return (
    <>
      <Button
        size="sm"
        onClick={() => {
          setDraft(linked);
          update.reset();
          setOpen(true);
        }}
      >
        <OnePasswordIcon size={13} /> 1Password
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title="Link a 1Password Environment"
        footer={
          <>
            {linked && (
              <Button
                variant="danger"
                disabled={update.isPending}
                onClick={() => setEnvironment("")}
              >
                Unlink
              </Button>
            )}
            <span className={settings.spacer} />
            <Button onClick={() => setOpen(false)} disabled={update.isPending}>
              Cancel
            </Button>
            <Button
              variant="primary"
              loading={update.isPending}
              disabled={!canLink}
              onClick={() => setEnvironment(id)}
            >
              {linked ? "Save" : "Link"}
            </Button>
          </>
        }
      >
        <form
          className={settings.fields}
          onSubmit={(e) => {
            e.preventDefault();
            if (canLink) setEnvironment(id);
          }}
        >
          <p className={settings.note}>
            shed loads this Environment's variables each time the service deploys. Variables you add
            here take priority over ones with the same name. After you change a value in 1Password,
            redeploy to use it.
          </p>
          {!connected && (
            <Callout tone="sunflower" icon={<PlugZap size={16} />}>
              Connect 1Password in{" "}
              <Link to="/settings" className={styles.link}>
                Settings
              </Link>{" "}
              first.
            </Callout>
          )}
          <Field
            label="Environment ID"
            hint="In the 1Password app, open Developer › Environments, choose the Environment, then Manage environment › Copy environment ID."
          >
            {(fieldId) => (
              <Input
                id={fieldId}
                mono
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                  update.reset();
                }}
                autoComplete="off"
                spellCheck={false}
              />
            )}
          </Field>
          {update.error && <p className={settings.error}>{errorMessage(update.error)}</p>}
        </form>
      </Dialog>
    </>
  );
}
