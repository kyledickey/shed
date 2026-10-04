import { useQuery } from "@tanstack/react-query";
import { branchesQuery } from "../../api/github";
import type { Service } from "../../api/types";
import { LayerCard } from "../../components/Card";
import { Field, Input, Select, Switch } from "../../components/Form";
import { SettingsCard, useSectionForm } from "./SectionForm";
import styles from "./Settings.module.css";

/** SourceCard edits where the code or image comes from; databases show their image read-only. */
export function SourceCard({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) =>
    fromRepo ? { repo: s.repo, branch: s.branch, rootDir: s.rootDir } : { image: s.image },
  );
  const { draft, set } = form;

  if (service.kind !== "app") {
    return (
      <LayerCard title="Source" padded>
        <div className={styles.fields}>
          <Field label="Image">
            {(id) => <Input id={id} mono value={service.image} readOnly />}
          </Field>
          <p className={styles.note}>
            Database images are managed by shed and can't be changed here.
          </p>
        </div>
      </LayerCard>
    );
  }

  return (
    <SettingsCard title="Source" meta="Where the code or image comes from." form={form}>
      {fromRepo ? (
        <>
          <div className={styles.twoCol}>
            <Field label="Repository" hint="owner/name">
              {(id) => (
                <Input
                  id={id}
                  mono
                  value={draft.repo ?? ""}
                  onChange={(e) => set("repo", e.target.value.trim())}
                />
              )}
            </Field>
            <BranchField
              repo={draft.repo ?? ""}
              value={draft.branch ?? ""}
              onChange={(b) => set("branch", b)}
            />
          </div>
          <Field label="Root directory" hint="Build from a subdirectory, e.g. apps/web.">
            {(id) => (
              <Input
                id={id}
                mono
                value={draft.rootDir ?? ""}
                onChange={(e) => set("rootDir", e.target.value)}
                placeholder="/"
              />
            )}
          </Field>
        </>
      ) : (
        <Field label="Image" hint="Pulled on every deploy.">
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.image ?? ""}
              onChange={(e) => set("image", e.target.value.trim())}
            />
          )}
        </Field>
      )}
    </SettingsCard>
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
  const names = branches.data ?? [];
  const options = (names.includes(value) || !value ? names : [value, ...names]).map((b) => ({
    value: b,
    label: b,
  }));
  return (
    <Field label="Branch">
      {(id) =>
        branches.isSuccess ? (
          <Select id={id} mono value={value} onChange={onChange} options={options} />
        ) : (
          <Input id={id} mono value={value} onChange={(e) => onChange(e.target.value.trim())} />
        )
      }
    </Field>
  );
}

/** BuildCard edits the Dockerfile path (repo apps) and start command. */
export function BuildCard({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) =>
    fromRepo
      ? { dockerfilePath: s.dockerfilePath, startCommand: s.startCommand }
      : { startCommand: s.startCommand },
  );
  const { draft, set } = form;
  return (
    <SettingsCard title={fromRepo ? "Build" : "Run"} form={form}>
      {fromRepo && (
        <Field
          label="Dockerfile path"
          hint="Leave empty to detect automatically: the Dockerfile is used if present, otherwise the app is built with Railpack."
        >
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.dockerfilePath ?? ""}
              onChange={(e) => set("dockerfilePath", e.target.value)}
              placeholder="Dockerfile"
            />
          )}
        </Field>
      )}
      <Field label="Start command" hint="Overrides the image's default command.">
        {(id) => (
          <Input
            id={id}
            mono
            value={draft.startCommand ?? ""}
            onChange={(e) => set("startCommand", e.target.value)}
            placeholder="npm start"
          />
        )}
      </Field>
    </SettingsCard>
  );
}

/** DeployCard edits deploy triggers, the healthcheck, and the container port. */
export function DeployCard({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) => ({
    port: s.port,
    healthcheckPath: s.healthcheckPath,
    autoDeploy: s.autoDeploy,
    waitForCi: s.waitForCi,
  }));
  const { draft, set } = form;
  return (
    <SettingsCard title="Deploy" form={form}>
      {fromRepo && (
        <>
          <Switch
            label="Auto deploy on push"
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
      <div className={styles.twoCol}>
        <Field
          label="Healthcheck path"
          hint="Must return 2xx or 3xx. Empty only checks that the port accepts connections."
        >
          {(id) => (
            <Input
              id={id}
              mono
              value={draft.healthcheckPath}
              onChange={(e) => set("healthcheckPath", e.target.value)}
              placeholder="/health"
            />
          )}
        </Field>
        <Field
          label="Container port"
          hint="0 detects the port from the image's EXPOSE. Injected as PORT."
        >
          {(id) => (
            <Input
              id={id}
              type="number"
              min={0}
              max={65535}
              mono
              value={draft.port}
              onChange={(e) => set("port", Number(e.target.value) || 0)}
            />
          )}
        </Field>
      </div>
    </SettingsCard>
  );
}
