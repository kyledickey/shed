import { Input, Select, Switch } from "@cloudflare/kumo";
import { useQuery } from "@tanstack/react-query";
import { branchesQuery } from "../../api/github";
import type { Service } from "../../api/types";
import { Rows, SettingRow } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { HelpTip } from "../docs/HelpTip";
import { NumberInput } from "./fields";
import styles from "./Form.module.css";
import { SettingsForm, useSectionForm } from "./SectionForm";

/** SourceSection edits where the code or image comes from; databases show their image read-only. */
export function SourceSection({ service }: { service: Service }) {
  if (service.kind !== "app") {
    return (
      <Rows>
        <SettingRow
          label="Image"
          description="Database images are managed by shed and can't be changed here."
          control={<span className={styles.value}>{service.image}</span>}
        />
      </Rows>
    );
  }
  return service.repo ? <RepoSource service={service} /> : <ImageSource service={service} />;
}

function RepoSource({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({
    repo: s.repo,
    branch: s.branch,
    rootDir: s.rootDir,
  }));
  const { draft, set } = form;
  return (
    <SettingsForm form={form}>
      <SettingRow
        label="Repository"
        description="The GitHub repository to build, as owner/name."
        control={
          <Input
            aria-label="Repository"
            className={cx(styles.wide, styles.mono)}
            value={draft.repo ?? ""}
            onChange={(e) => set("repo", e.target.value.trim())}
            spellCheck={false}
          />
        }
      />
      <SettingRow
        label="Branch"
        description="Deployments build the latest commit on this branch."
        control={
          <BranchControl
            repo={draft.repo ?? ""}
            value={draft.branch ?? ""}
            onChange={(b) => set("branch", b)}
          />
        }
      />
      <SettingRow
        label="Root directory"
        description="Build from a subdirectory, e.g. apps/web."
        control={
          <Input
            aria-label="Root directory"
            className={cx(styles.wide, styles.mono)}
            value={draft.rootDir ?? ""}
            onChange={(e) => set("rootDir", e.target.value)}
            placeholder="/"
            spellCheck={false}
          />
        }
      />
    </SettingsForm>
  );
}

function ImageSource({ service }: { service: Service }) {
  const form = useSectionForm(service, (s) => ({ image: s.image }));
  return (
    <SettingsForm form={form}>
      <SettingRow
        label="Image"
        description="Pulled on every deploy."
        control={
          <Input
            aria-label="Image"
            className={cx(styles.wide, styles.mono)}
            value={form.draft.image ?? ""}
            onChange={(e) => form.set("image", e.target.value.trim())}
            spellCheck={false}
          />
        }
      />
    </SettingsForm>
  );
}

function BranchControl({
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
  if (!branches.isSuccess) {
    return (
      <Input
        aria-label="Branch"
        className={cx(styles.wide, styles.mono)}
        value={value}
        onChange={(e) => onChange(e.target.value.trim())}
        spellCheck={false}
      />
    );
  }
  const options = names.includes(value) || !value ? names : [value, ...names];
  return (
    <Select
      aria-label="Branch"
      className={cx(styles.wide, styles.mono)}
      value={value}
      onValueChange={(v) => v && onChange(v)}
      items={options.map((b) => ({ value: b, label: b }))}
    />
  );
}

/** BuildSection edits the Dockerfile path (repo apps) and start command. */
export function BuildSection({ service }: { service: Service }) {
  const fromRepo = !!service.repo;
  const form = useSectionForm(service, (s) =>
    fromRepo
      ? { dockerfilePath: s.dockerfilePath, startCommand: s.startCommand }
      : { startCommand: s.startCommand },
  );
  const { draft, set } = form;
  return (
    <SettingsForm form={form}>
      {fromRepo && (
        <SettingRow
          label={
            <>
              <HelpTip topic="buildDetection">Dockerfile path</HelpTip>
            </>
          }
          description="Leave empty to detect: the Dockerfile is used if present, otherwise Railpack builds the app."
          control={
            <Input
              aria-label="Dockerfile path"
              className={cx(styles.wide, styles.mono)}
              value={draft.dockerfilePath ?? ""}
              onChange={(e) => set("dockerfilePath", e.target.value)}
              placeholder="Dockerfile"
              spellCheck={false}
            />
          }
        />
      )}
      <SettingRow
        label="Start command"
        description="Overrides the image's default command."
        control={
          <Input
            aria-label="Start command"
            className={cx(styles.wide, styles.mono)}
            value={draft.startCommand ?? ""}
            onChange={(e) => set("startCommand", e.target.value)}
            placeholder="npm start"
            spellCheck={false}
          />
        }
      />
    </SettingsForm>
  );
}

/** DeploySection edits deploy triggers, the healthcheck, and the container port. */
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
    <SettingsForm form={form}>
      {fromRepo && (
        <>
          <SettingRow
            label={
              <>
                <HelpTip topic="autoDeploy">Auto deploy on push</HelpTip>
              </>
            }
            description={`Deploy automatically when ${service.branch} changes.`}
            control={
              <Switch
                aria-label="Auto deploy on push"
                checked={draft.autoDeploy}
                onCheckedChange={(v) => set("autoDeploy", v)}
              />
            }
          />
          <SettingRow
            label={
              <>
                <HelpTip topic="waitForCi">Wait for CI</HelpTip>
              </>
            }
            description="Hold push deployments until GitHub checks pass. Failed checks skip the deploy."
            control={
              <Switch
                aria-label="Wait for CI"
                checked={draft.waitForCi}
                disabled={!draft.autoDeploy}
                onCheckedChange={(v) => set("waitForCi", v)}
              />
            }
          />
        </>
      )}
      <SettingRow
        label={
          <>
            <HelpTip topic="healthchecks">Healthcheck path</HelpTip>
          </>
        }
        description="Must return 2xx or 3xx. Empty only checks that the port accepts connections."
        control={
          <Input
            aria-label="Healthcheck path"
            className={cx(styles.wide, styles.mono)}
            value={draft.healthcheckPath}
            onChange={(e) => set("healthcheckPath", e.target.value)}
            placeholder="/health"
            spellCheck={false}
          />
        }
      />
      <SettingRow
        label="Container port"
        description="0 detects the port from the image's EXPOSE. Injected as PORT."
        control={
          <NumberInput
            aria-label="Container port"
            min={0}
            max={65535}
            value={draft.port}
            onChange={(e) => set("port", Number(e.target.value) || 0)}
          />
        }
      />
    </SettingsForm>
  );
}
