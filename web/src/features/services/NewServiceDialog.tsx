import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Box, CircleX, Database, ExternalLink, Leaf, Lock, Search, Zap } from "lucide-react";
import { useId, useState, type ReactNode } from "react";
import { setupQuery } from "../../api/auth";
import { errorMessage } from "../../api/client";
import { branchesQuery, reposQuery } from "../../api/github";
import { useCreateService } from "../../api/services";
import type { NewService, Repo, ServiceKind } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Button, LinkButton } from "../../components/Button";
import { Field, Input, Select } from "../../components/Form";
import { Callout, GitHubIcon, ServiceIcon, Skeleton } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import type { Tone } from "../../components/tone";
import { imageName, repoName, toServiceName } from "../../lib/names";
import styles from "./NewServiceDialog.module.css";

type Source = "repo" | "image" | Exclude<ServiceKind, "app">;

type Glyph = (props: { size?: number }) => ReactNode;

const sources: { source: Source; name: string; hint: string; Icon: Glyph; tone: Tone }[] = [
  {
    source: "repo",
    name: "GitHub repo",
    hint: "Build and deploy on push",
    Icon: GitHubIcon,
    tone: "grape",
  },
  { source: "image", name: "Docker image", hint: "Any public image", Icon: Box, tone: "teal" },
  {
    source: "postgres",
    name: "Postgres",
    hint: "Relational database",
    Icon: Database,
    tone: "sky",
  },
  {
    source: "mysql",
    name: "MySQL",
    hint: "Relational database",
    Icon: Database,
    tone: "tangerine",
  },
  { source: "mongo", name: "MongoDB", hint: "Document database", Icon: Leaf, tone: "grass" },
  { source: "redis", name: "Redis", hint: "Key-value store", Icon: Zap, tone: "tomato" },
];

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

/** NewServiceDialog adds a repo app, an image app or a database to a project. */
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

  const picked = sources.find((s) => s.source === source);

  return (
    <Dialog
      open={open}
      onOpenChange={change}
      size="md"
      title={picked ? `New ${picked.name}` : "New service"}
      description={source ? descriptions[source] : `Add an app or a database to ${projectName}.`}
      icon={picked && <picked.Icon size={18} />}
      iconTone={picked?.tone}
      footer={
        source ? (
          <>
            <Button variant="ghost" onClick={reset} className={styles.back}>
              Back
            </Button>
            <Button
              type="submit"
              form={formId}
              variant="primary"
              disabled={!body}
              loading={create.isPending}
            >
              Create and deploy
            </Button>
          </>
        ) : (
          <Button variant="ghost" onClick={() => change(false)}>
            Cancel
          </Button>
        )
      }
    >
      {!source ? (
        <div className={styles.kindGrid}>
          {sources.map((s) => (
            <button
              key={s.source}
              type="button"
              className={styles.kindTile}
              data-tone={s.tone}
              onClick={() => setSource(s.source)}
            >
              <ServiceIcon icon={<s.Icon />} tone={s.tone} size={30} />
              <span>
                <span className={styles.kindName}>{s.name}</span>
                <span className={styles.kindHint}>{s.hint}</span>
              </span>
            </button>
          ))}
        </div>
      ) : (
        <form
          id={formId}
          className={styles.form}
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {source === "repo" &&
            (repo ? (
              <>
                <div className={styles.selected}>
                  <GitHubIcon size={14} />
                  <span className={styles.repoName}>{repo.fullName}</span>
                  <Button size="sm" variant="ghost" onClick={() => setRepo(null)}>
                    Change
                  </Button>
                </div>
                <BranchField repo={repo.fullName} value={branch} onChange={setBranch} />
              </>
            ) : (
              <RepoPicker
                onPick={(r) => {
                  setRepo(r);
                  setBranch(r.defaultBranch);
                  setCustomName(null);
                }}
              />
            ))}
          {source === "image" && (
            <Field
              label="Image"
              hint="Any image reference, e.g. nginx:alpine or ghcr.io/owner/app."
            >
              {(id) => (
                <Input
                  id={id}
                  mono
                  value={image}
                  onChange={(e) => setImage(e.target.value)}
                  placeholder="nginx:alpine"
                  autoComplete="off"
                  autoFocus
                />
              )}
            </Field>
          )}
          {(source !== "repo" || repo) && (
            <Field
              label="Service name"
              hint="Also the private hostname other services use to reach it."
              error={nameError}
            >
              {(id) => (
                <Input
                  id={id}
                  mono
                  value={name}
                  autoComplete="off"
                  autoFocus={source !== "image" && source !== "repo"}
                  onChange={(e) =>
                    setCustomName(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))
                  }
                />
              )}
            </Field>
          )}
          {create.error && (
            <Callout tone="tomato" icon={<CircleX size={16} />} title="Couldn't create service">
              {errorMessage(create.error)}
            </Callout>
          )}
        </form>
      )}
    </Dialog>
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
  const options = (names.includes(value) || !value ? names : [value, ...names]).map((b) => ({
    value: b,
    label: b,
  }));
  return (
    <Field
      label="Branch"
      hint="Pushes to this branch deploy automatically."
      error={branches.error && `Couldn't load branches: ${errorMessage(branches.error)}`}
    >
      {(id) => (
        <Select
          id={id}
          mono
          value={value}
          onChange={onChange}
          options={options}
          disabled={branches.isPending}
        />
      )}
    </Field>
  );
}

function RepoPicker({ onPick }: { onPick: (repo: Repo) => void }) {
  const repos = useQuery(reposQuery);
  const setup = useQuery(setupQuery);
  const [filter, setFilter] = useState("");
  const installUrl = setup.data?.installUrl;

  if (repos.isPending) {
    return (
      <div className={styles.repos} aria-busy>
        {[70, 55, 62, 48].map((w) => (
          <div key={w} className={styles.skeletonRow}>
            <Skeleton width={`${w}%`} height={12} />
          </div>
        ))}
      </div>
    );
  }

  if (repos.isError || repos.data.length === 0) {
    return (
      <Callout
        tone="sunflower"
        icon={<GitHubIcon size={16} />}
        title={repos.isError ? "Couldn't load repositories" : "No repositories available"}
        actions={
          installUrl && (
            <LinkButton size="sm" href={installUrl} target="_blank" rel="noreferrer">
              Configure access
              <ExternalLink size={14} />
            </LinkButton>
          )
        }
      >
        {repos.isError && <span className={styles.detail}>{errorMessage(repos.error)}</span>}
        The GitHub App needs access to the repositories you want to deploy. Grant it on GitHub, then
        come back here.
      </Callout>
    );
  }

  const query = filter.trim().toLowerCase();
  const matches = repos.data.filter((r) => r.fullName.toLowerCase().includes(query));

  return (
    <div className={styles.picker}>
      <Input
        prefix={<Search size={14} />}
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Search repositories"
        aria-label="Search repositories"
        autoComplete="off"
        autoFocus
      />
      <ul className={styles.repos}>
        {matches.map((r) => (
          <li key={r.fullName}>
            <button type="button" className={styles.repo} onClick={() => onPick(r)}>
              <GitHubIcon size={14} />
              <span className={styles.repoName}>{r.fullName}</span>
              {r.private && (
                <Badge size="sm" icon={<Lock size={11} />}>
                  Private
                </Badge>
              )}
            </button>
          </li>
        ))}
        {matches.length === 0 && (
          <li className={styles.noMatch}>No repositories match “{filter.trim()}”.</li>
        )}
      </ul>
      {installUrl && (
        <p className={styles.hint}>
          Missing a repository?{" "}
          <a href={installUrl} target="_blank" rel="noreferrer">
            Adjust GitHub App access
          </a>
        </p>
      )}
    </div>
  );
}
