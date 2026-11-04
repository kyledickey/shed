import { Button, InputGroup, Input, LinkButton, Loader, Select } from "@cloudflare/kumo";
import {
  CubeIcon,
  DatabaseIcon,
  GithubLogoIcon,
  LeafIcon,
  LightningIcon,
  MagnifyingGlassIcon,
  type Icon,
} from "@phosphor-icons/react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useId, useState } from "react";
import { setupQuery } from "../../api/auth";
import { errorMessage } from "../../api/client";
import { branchesQuery, reposQuery } from "../../api/github";
import { useCreateService } from "../../api/services";
import type { NewService, Repo, ServiceKind } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { Pill } from "../../components/Pill";
import { imageName, repoName, toServiceName } from "../../lib/names";
import { FormDialog } from "../settings/FormDialog";
import styles from "./NewServiceDialog.module.css";

type Source = "repo" | "image" | Exclude<ServiceKind, "app">;

type Method = { source: Source; name: string; Glyph: Icon };

const apps: Method[] = [
  { source: "repo", name: "GitHub repository", Glyph: GithubLogoIcon },
  { source: "image", name: "Docker image", Glyph: CubeIcon },
];

const databases: Method[] = [
  { source: "postgres", name: "Postgres", Glyph: DatabaseIcon },
  { source: "mysql", name: "MySQL", Glyph: DatabaseIcon },
  { source: "mongo", name: "MongoDB", Glyph: LeafIcon },
  { source: "redis", name: "Redis", Glyph: LightningIcon },
];

const methods = [...apps, ...databases];

const descriptions: Record<Source, string> = {
  repo: "Builds from a Dockerfile, or with Railpack when there isn't one. Pushes deploy automatically.",
  image: "Runs an image from Docker Hub or another registry.",
  postgres: "Runs with a persistent volume. Connection variables are generated for you.",
  mysql: "Runs with a persistent volume. Connection variables are generated for you.",
  mongo: "Runs with a persistent volume. Connection variables are generated for you.",
  redis: "Runs with a persistent volume. Connection variables are generated for you.",
};

const dnsLabel = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

type NewServiceDialogProps = {
  projectId: string;
  projectName: string;
  /** Names already used in the project. */
  taken: string[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

/**
 * NewServiceDialog adds a repo app, an image app or a database to a
 * project in a dialog: choose a source, pick a repository if it is one,
 * then name it and deploy.
 */
export function NewServiceDialog({
  projectId,
  projectName,
  taken,
  open,
  onOpenChange,
}: NewServiceDialogProps) {
  const formId = useId();
  const [source, setSource] = useState<Source | null>(null);
  const [repo, setRepo] = useState<Repo | null>(null);
  const [branch, setBranch] = useState("");
  const [image, setImage] = useState("");
  const [customName, setCustomName] = useState<string | null>(null);
  const create = useCreateService(projectId);
  const navigate = useNavigate();
  const toast = useToast();
  const setup = useQuery(setupQuery);
  const installUrl = setup.data?.installUrl;

  const suggestion =
    source === "repo"
      ? repo
        ? repoName(repo.fullName)
        : ""
      : source === "image"
        ? imageName(image.trim())
        : (source ?? "");
  const name = customName ?? toServiceName(suggestion);
  const nameError = !name
    ? undefined
    : !dnsLabel.test(name)
      ? "Use lowercase letters, numbers and hyphens, starting and ending with a letter or number."
      : taken.includes(name) && !create.isSuccess
        ? `${projectName} already has a service named ${name}.`
        : undefined;

  const body = ((): NewService | null => {
    if (!source || !name || nameError) return null;
    if (source === "repo") {
      return repo && branch ? { kind: "app", name, repo: repo.fullName, branch } : null;
    }
    if (source === "image") return image.trim() ? { kind: "app", name, image: image.trim() } : null;
    return { kind: source, name };
  })();

  const reset = () => {
    setSource(null);
    setRepo(null);
    setBranch("");
    setImage("");
    setCustomName(null);
    create.reset();
  };

  const change = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const submit = () => {
    if (!body) return;
    create.mutate(body, {
      onSuccess: (service) => {
        toast.add({
          title: "Service created",
          description: `${service.name} is deploying.`,
          type: "success",
        });
        void navigate({
          to: "/projects/$projectId/services/$serviceId",
          params: { projectId, serviceId: service.id },
        });
      },
    });
  };

  const picked = methods.find((m) => m.source === source);
  const picking = source === "repo" && !repo;
  const title = !picked
    ? "Add a service"
    : picking
      ? "Select a repository"
      : source === "repo" || source === "image"
        ? `Deploy a ${picked.name}`
        : `Add ${picked.name}`;

  return (
    <FormDialog
      open={open}
      onOpenChange={change}
      title={title}
      description={
        source ? descriptions[source] : `Deploy an app or add a database to ${projectName}.`
      }
      footer={
        source && (
          <>
            <Button variant="secondary" onClick={reset}>
              Back
            </Button>
            {!picking && (
              <Button
                type="submit"
                form={formId}
                variant="primary"
                disabled={!body}
                loading={create.isPending}
              >
                Create and deploy
              </Button>
            )}
          </>
        )
      }
    >
      {!source ? (
        <>
          <MethodGroup label="Deploy an app" methods={apps} onPick={setSource} />
          <MethodGroup label="Add a database" methods={databases} onPick={setSource} />
        </>
      ) : picking ? (
        <RepoPicker
          installUrl={installUrl}
          onPick={(r) => {
            setRepo(r);
            setBranch(r.defaultBranch);
            setCustomName(null);
          }}
        />
      ) : (
        <form
          id={formId}
          className={styles.form}
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {repo && (
            <>
              <div className={styles.selected}>
                <GithubLogoIcon size={16} />
                <span className={styles.repoName}>{repo.fullName}</span>
                <Button size="sm" variant="ghost" onClick={() => setRepo(null)}>
                  Change
                </Button>
              </div>
              <BranchField repo={repo.fullName} value={branch} onChange={setBranch} />
            </>
          )}
          {source === "image" && (
            <Input
              label="Image"
              description="Any image reference, such as nginx:alpine or ghcr.io/owner/app."
              className={styles.mono}
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="nginx:alpine"
              autoComplete="off"
              autoFocus
            />
          )}
          <Input
            label="Service name"
            description="Also the private hostname other services use to reach it."
            error={nameError}
            className={styles.mono}
            value={name}
            autoComplete="off"
            autoFocus={source !== "image" && source !== "repo"}
            onChange={(e) =>
              setCustomName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))
            }
          />
          {create.error && (
            <p className={styles.error} role="alert">
              Couldn't create the service: {errorMessage(create.error)}
            </p>
          )}
        </form>
      )}
    </FormDialog>
  );
}

function MethodGroup({
  label,
  methods,
  onPick,
}: {
  label: string;
  methods: Method[];
  onPick: (source: Source) => void;
}) {
  return (
    <div className={styles.group} role="group" aria-label={label}>
      <span className={styles.groupLabel}>{label}</span>
      <div className={styles.methods}>
        {methods.map(({ source, name, Glyph }) => (
          <button
            key={source}
            type="button"
            className={styles.method}
            onClick={() => onPick(source)}
          >
            <span className={styles.glyph}>
              <Glyph size={16} />
            </span>
            {name}
          </button>
        ))}
      </div>
    </div>
  );
}

function BranchField({
  repo,
  value,
  onChange,
}: {
  repo: string;
  value: string;
  onChange: (branch: string) => void;
}) {
  const branches = useQuery(branchesQuery(repo));
  const names = branches.data ?? [];
  const items = (names.includes(value) || !value ? names : [value, ...names]).map((b) => ({
    value: b,
    label: b,
  }));
  return (
    <Select
      label="Branch"
      description="Pushes to this branch deploy automatically."
      error={branches.error ? `Couldn't load branches: ${errorMessage(branches.error)}` : undefined}
      className={styles.branch}
      value={value}
      onValueChange={(v) => typeof v === "string" && onChange(v)}
      items={items}
      loading={branches.isPending}
      disabled={branches.isPending}
    />
  );
}

function RepoPicker({
  installUrl,
  onPick,
}: {
  installUrl: string | undefined;
  onPick: (repo: Repo) => void;
}) {
  const repos = useQuery(reposQuery);
  const [filter, setFilter] = useState("");

  if (repos.isPending) {
    return (
      <div className={styles.loading} aria-busy>
        <Loader />
      </div>
    );
  }

  if (repos.isError || repos.data.length === 0) {
    return (
      <div className={styles.well}>
        <span>
          {repos.isError
            ? `Couldn't load repositories: ${errorMessage(repos.error)}`
            : "The GitHub App can't see any repositories yet. Grant it access on GitHub, then come back here."}
        </span>
        {installUrl && (
          <LinkButton size="sm" href={installUrl} target="_blank" rel="noreferrer" external>
            Configure access
          </LinkButton>
        )}
      </div>
    );
  }

  const query = filter.trim().toLowerCase();
  const matches = repos.data.filter((r) => r.fullName.toLowerCase().includes(query));

  return (
    <div className={styles.picker}>
      <InputGroup>
        <InputGroup.Addon>
          <MagnifyingGlassIcon size={16} />
        </InputGroup.Addon>
        <InputGroup.Input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Search repositories"
          aria-label="Search repositories"
          autoComplete="off"
          autoFocus
        />
      </InputGroup>
      <ul className={styles.repos}>
        {matches.map((r) => (
          <li key={r.fullName}>
            <button type="button" className={styles.repo} onClick={() => onPick(r)}>
              <GithubLogoIcon size={16} className={styles.repoGlyph} />
              <span className={styles.repoName}>{r.fullName}</span>
              {r.private && <Pill>Private</Pill>}
            </button>
          </li>
        ))}
        {matches.length === 0 && (
          <li className={styles.noMatch}>No repositories match “{filter.trim()}”.</li>
        )}
      </ul>
      {installUrl && (
        <p className={styles.note}>
          Missing a repository?{" "}
          <a href={installUrl} target="_blank" rel="noreferrer">
            Adjust GitHub App access
          </a>
        </p>
      )}
    </div>
  );
}
