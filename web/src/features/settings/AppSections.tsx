import { useQuery } from "@tanstack/react-query";
import { branchesQuery } from "../../api/github";
import type { Service } from "../../api/types";
import { Field, FieldRow } from "../../components/Field";
import { Input } from "../../components/Input";
import { Select } from "../../components/Select";
import { Switch } from "../../components/Switch";
import { SettingsSection, useSectionForm } from "./SettingsSection";

export function SourceSection({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) =>
    fromRepo ? { repo: s.repo, branch: s.branch, rootDir: s.rootDir } : { image: s.image },
  );
  const { draft, set } = form;

  return (
    <SettingsSection
      title="Source"
      description="Where this service's code or image comes from."
      form={form}
    >
      {fromRepo ? (
        <>
          <FieldRow>
            <Field label="Repository" hint="owner/name">
              <Input
                mono
                value={draft.repo ?? ""}
                onChange={(e) => set("repo", e.target.value.trim())}
              />
            </Field>
            <BranchField
              repo={draft.repo ?? ""}
              value={draft.branch ?? ""}
              onChange={(b) => set("branch", b)}
            />
          </FieldRow>
          <Field
            label="Root directory"
            hint="Build from a subdirectory of the repository, e.g. apps/web."
          >
            <Input
              mono
              value={draft.rootDir ?? ""}
              onChange={(e) => set("rootDir", e.target.value)}
              placeholder="/"
            />
          </Field>
        </>
      ) : (
        <Field label="Image" hint="Pulled on every deploy.">
          <Input
            mono
            value={draft.image ?? ""}
            onChange={(e) => set("image", e.target.value.trim())}
          />
        </Field>
      )}
    </SettingsSection>
  );
}

function BranchField({
  repo,
  value,
  onChange,
}: {
  repo: string;
  value: string;
  onChange: (v: string) => void;
}) {
  const branches = useQuery(branchesQuery(repo));
  const options = branches.data ?? [];
  return (
    <Field label="Branch">
      {branches.isSuccess ? (
        <Select
          mono
          value={value}
          onChange={onChange}
          options={options.includes(value) ? options : [value, ...options]}
        />
      ) : (
        <Input mono value={value} onChange={(e) => onChange(e.target.value.trim())} />
      )}
    </Field>
  );
}

export function BuildSection({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) =>
    fromRepo
      ? { dockerfilePath: s.dockerfilePath, startCommand: s.startCommand }
      : { startCommand: s.startCommand },
  );
  const { draft, set } = form;

  return (
    <SettingsSection
      title={fromRepo ? "Build" : "Run"}
      description={fromRepo ? "Uses the Dockerfile if one exists, otherwise Railpack." : undefined}
      form={form}
    >
      {fromRepo && (
        <Field label="Dockerfile path" hint="Leave empty to detect automatically.">
          <Input
            mono
            value={draft.dockerfilePath ?? ""}
            onChange={(e) => set("dockerfilePath", e.target.value)}
            placeholder="Dockerfile"
          />
        </Field>
      )}
      <Field label="Start command" hint="Overrides the image's default command.">
        <Input
          mono
          value={draft.startCommand ?? ""}
          onChange={(e) => set("startCommand", e.target.value)}
          placeholder="npm start"
        />
      </Field>
    </SettingsSection>
  );
}

export function DeploySection({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) => ({
    port: s.port,
    healthcheckPath: s.healthcheckPath,
    autoDeploy: s.autoDeploy,
    waitForCi: s.waitForCi,
  }));
  const { draft, set } = form;

  return (
    <SettingsSection title="Deploy" form={form}>
      <FieldRow>
        <Field label="Port" hint="The port your app listens on. Injected as PORT.">
          <Input
            type="number"
            min={0}
            max={65535}
            mono
            value={draft.port || ""}
            onChange={(e) => set("port", Number(e.target.value) || 0)}
            placeholder="None"
          />
        </Field>
        <Field label="Healthcheck path" hint="Must return 2xx or 3xx. Empty checks the port only.">
          <Input
            mono
            value={draft.healthcheckPath}
            onChange={(e) => set("healthcheckPath", e.target.value)}
            placeholder="/health"
          />
        </Field>
      </FieldRow>
      {fromRepo && (
        <>
          <Switch
            label="Deploy on push"
            description={`Deploy automatically when ${service.branch} changes.`}
            checked={draft.autoDeploy}
            onChange={(v) => set("autoDeploy", v)}
          />
          <Switch
            label="Wait for CI"
            description="Hold push deployments until GitHub checks pass. Failed checks skip the deploy."
            checked={draft.waitForCi}
            disabled={!draft.autoDeploy}
            onChange={(v) => set("waitForCi", v)}
          />
        </>
      )}
    </SettingsSection>
  );
}
