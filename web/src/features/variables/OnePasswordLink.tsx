import { CircleX } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useLinkOnePassword, useUnlinkOnePassword } from "../../api/onepassword";
import type { Service } from "../../api/types";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { Callout, OnePasswordIcon } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import settings from "../settings/Settings.module.css";
import { useRedeployHint } from "../services/redeploy";

/**
 * OnePasswordLink is a button that opens a dialog to link a 1Password
 * Environment with its service account token, change the link, or unlink it.
 */
export function OnePasswordLink({
  service,
  open,
  onOpenChange,
}: {
  service: Service;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const linked = service.onePassword;
  const [environment, setEnvironment] = useState("");
  const [token, setToken] = useState("");
  const link = useLinkOnePassword(service.id);
  const unlink = useUnlinkOnePassword(service.id);
  const hint = useRedeployHint();
  const toast = useToast();

  // Each opening starts from the saved link.
  const [shown, setShown] = useState(open);
  if (open !== shown) {
    setShown(open);
    if (open) {
      setEnvironment(linked?.environment ?? "");
      setToken("");
      link.reset();
      unlink.reset();
    }
  }

  const id = environment.trim();
  const secret = token.trim();
  const hasToken = linked?.hasToken ?? false;
  const changed = id !== linked?.environment || secret !== "";
  const canLink = id !== "" && (secret !== "" || hasToken) && changed;
  const pending = link.isPending || unlink.isPending;

  const done = (title: string) => {
    onOpenChange(false);
    hint.markPending();
    toast.add({ type: "success", title, description: "Redeploy to apply." });
  };
  const save = () => {
    if (!canLink) return;
    link.mutate(
      { environment: id, token: secret || undefined },
      { onSuccess: () => done(linked ? "1Password link saved" : "Environment linked") },
    );
  };

  return (
    <>
      <Button size="sm" onClick={() => onOpenChange(true)}>
        <OnePasswordIcon size={13} /> 1Password
      </Button>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title="Link a 1Password Environment"
        footer={
          <>
            {linked && (
              <Button
                variant="danger"
                disabled={pending}
                onClick={() =>
                  unlink.mutate(undefined, { onSuccess: () => done("Environment unlinked") })
                }
              >
                Unlink
              </Button>
            )}
            <span className={settings.spacer} />
            <Button onClick={() => onOpenChange(false)} disabled={pending}>
              Cancel
            </Button>
            <Button
              variant="primary"
              loading={link.isPending}
              disabled={!canLink || unlink.isPending}
              onClick={save}
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
            save();
          }}
        >
          <p className={settings.note}>
            shed loads this Environment's variables each time the service deploys, using a service
            account that can read only this Environment. Variables you add here take priority over
            ones with the same name. After you change a value in 1Password, redeploy to use it.
          </p>
          <Field
            label="Environment ID"
            hint="In the 1Password app, open Developer › Environments, choose the Environment, then Manage environment › Copy environment ID."
          >
            {(fieldId) => (
              <Input
                id={fieldId}
                mono
                value={environment}
                onChange={(e) => {
                  setEnvironment(e.target.value);
                  link.reset();
                }}
                autoComplete="off"
                spellCheck={false}
              />
            )}
          </Field>
          <Field
            label="Service account token"
            hint={
              hasToken
                ? "Leave blank to keep the saved token."
                : "On 1Password.com, create a service account with read access to this Environment. Its access can't be changed later, so use one per Environment."
            }
          >
            {(fieldId) => (
              <Input
                id={fieldId}
                mono
                type="password"
                value={token}
                onChange={(e) => {
                  setToken(e.target.value);
                  link.reset();
                }}
                placeholder={hasToken ? "••••••• (saved)" : "ops_…"}
                autoComplete="off"
                spellCheck={false}
              />
            )}
          </Field>
          {link.error && (
            <Callout
              tone="tomato"
              icon={<CircleX size={16} />}
              title="Couldn't link the Environment"
            >
              {errorMessage(link.error)}
            </Callout>
          )}
          {unlink.error && (
            <Callout
              tone="tomato"
              icon={<CircleX size={16} />}
              title="Couldn't unlink the Environment"
            >
              {errorMessage(unlink.error)}
            </Callout>
          )}
          <button type="submit" hidden />
        </form>
      </Dialog>
    </>
  );
}
