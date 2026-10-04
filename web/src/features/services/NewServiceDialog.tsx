import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Container, Database, ExternalLink, Lock } from "lucide-react";
import { useState, type ReactNode } from "react";
import { setupQuery } from "../../api/auth";
import { branchesQuery, reposQuery } from "../../api/github";
import { useCreateService } from "../../api/services";
import { databaseKinds, type NewService, type Repo, type ServiceKind } from "../../api/types";
import { ErrorText } from "../../components/Banner";
import { Button, LinkButton } from "../../components/Button";
import { Dialog, DialogBody, DialogFooter, DialogForm } from "../../components/Dialog";
import { EmptyState } from "../../components/EmptyState";
import { Field } from "../../components/Field";
import { GitHubIcon } from "../../components/GitHubIcon";
import { Input, SearchInput } from "../../components/Input";
import { Select } from "../../components/Select";
import { kindLabels } from "../../components/ServiceIcon";
import { imageName, repoName, toServiceName } from "../../lib/names";
import styles from "./NewServiceDialog.module.css";

type Source = "repo" | "database" | "image";

type NewServiceDialogProps = {
  projectId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function NewServiceDialog({ projectId, open, onOpenChange }: NewServiceDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="New service" size="md">
      <NewServiceFlow projectId={projectId} />
    </Dialog>
  );
}

function NewServiceFlow({ projectId }: { projectId: string }) {
  const [source, setSource] = useState<Source | null>(null);
  const create = useCreateService(projectId);
  const navigate = useNavigate();

  if (!source) return <SourcePicker onPick={setSource} />;

  const props: StepProps = {
    onBack: () => {
      setSource(null);
      create.reset();
    },
    onSubmit: (body) =>
      create.mutate(body, {
        onSuccess: (service) =>
          navigate({
            to: "/projects/$projectId/services/$serviceId",
            params: { projectId, serviceId: service.id },
          }),
      }),
    pending: create.isPending,
    error: create.error,
  };
  if (source === "repo") return <RepoStep {...props} />;
  if (source === "database") return <DatabaseStep {...props} />;
  return <ImageStep {...props} />;
}

function SourcePicker({ onPick }: { onPick: (source: Source) => void }) {
  const options: { source: Source; icon: ReactNode; title: string; text: string }[] = [
    {
      source: "repo",
      icon: <GitHubIcon size={16} />,
      title: "GitHub repository",
      text: "Build from a Dockerfile or automatically with Railpack. Deploys on push.",
    },
    {
      source: "database",
      icon: <Database size={16} />,
      title: "Database",
      text: "PostgreSQL, MySQL, MongoDB, or Redis with a persistent volume.",
    },
    {
      source: "image",
      icon: <Container size={16} />,
      title: "Docker image",
      text: "Run any public image from Docker Hub or another registry.",
    },
  ];
  return (
    <DialogBody>
      <div className={styles.options}>
        {options.map((o) => (
          <button
            key={o.source}
            type="button"
            className={styles.option}
            onClick={() => onPick(o.source)}
          >
            <span className={styles.optionIcon}>{o.icon}</span>
            <span>
              <span className={styles.optionTitle}>{o.title}</span>
              <span className={styles.optionText}>{o.text}</span>
            </span>
          </button>
        ))}
      </div>
    </DialogBody>
  );
}

type StepProps = {
  onBack: () => void;
  onSubmit: (body: NewService) => void;
  pending: boolean;
  error: Error | null;
};

/** A service name that follows a suggestion until the user edits it. */
function useServiceName(suggestion: string) {
  const [custom, setCustom] = useState<string | null>(null);
  const value = custom ?? toServiceName(suggestion);
  return { value, set: setCustom, final: toServiceName(value) };
}

function StepForm({
  onBack,
  pending,
  error,
  canSubmit,
  onSubmit,
  children,
}: Omit<StepProps, "onSubmit"> & {
  canSubmit: boolean;
  onSubmit: () => void;
  children: ReactNode;
}) {
  return (
    <DialogForm onSubmit={() => canSubmit && onSubmit()}>
      <DialogBody>
        {children}
        <ErrorText error={error} />
      </DialogBody>
      <DialogFooter>
        <Button onClick={onBack}>Back</Button>
        <Button type="submit" variant="primary" disabled={!canSubmit} loading={pending}>
          Create and deploy
        </Button>
      </DialogFooter>
    </DialogForm>
  );
}

function NameField({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return (
    <Field label="Service name" hint="Also the private hostname other services use to reach it.">
      <Input
        mono
        value={value}
        onChange={(e) => onChange(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "-"))}
      />
    </Field>
  );
}

function RepoStep(props: StepProps) {
  const [repo, setRepo] = useState<Repo | null>(null);
  const [branch, setBranch] = useState("");
  const name = useServiceName(repo ? repoName(repo.fullName) : "");
  const branches = useQuery(branchesQuery(repo?.fullName ?? ""));

  return (
    <StepForm
      {...props}
      canSubmit={!!repo && !!branch && !!name.final}
      onSubmit={() =>
        repo && props.onSubmit({ kind: "app", name: name.final, repo: repo.fullName, branch })
      }
    >
      {repo ? (
        <>
          <div className={styles.selected}>
            <GitHubIcon size={14} />
            <span className={styles.repoName}>{repo.fullName}</span>
            <Button size="sm" variant="ghost" onClick={() => setRepo(null)}>
              Change
            </Button>
          </div>
          <Field label="Branch" hint="Pushes to this branch deploy automatically.">
            <Select
              mono
              value={branch}
              onChange={setBranch}
              options={branches.data ?? [branch]}
              disabled={branches.isPending}
            />
          </Field>
          <NameField value={name.value} onChange={name.set} />
        </>
      ) : (
        <RepoPicker
          onPick={(picked) => {
            setRepo(picked);
            setBranch(picked.defaultBranch);
            name.set(null);
          }}
        />
      )}
    </StepForm>
  );
}

function RepoPicker({ onPick }: { onPick: (repo: Repo) => void }) {
  const repos = useQuery(reposQuery);
  const setup = useQuery(setupQuery);
  const [filter, setFilter] = useState("");

  if (repos.isPending) return <EmptyState compact title="Loading repositories…" />;
  if (repos.isError) return <ErrorText error={repos.error} />;

  const installUrl = setup.data?.installUrl;
  if (repos.data.length === 0) {
    return (
      <EmptyState
        compact
        title="No repositories available"
        action={
          installUrl && (
            <LinkButton href={installUrl} target="_blank" rel="noreferrer">
              Configure GitHub App
              <ExternalLink size={14} />
            </LinkButton>
          )
        }
      >
        Install the GitHub App on the repositories you want to deploy, then come back here.
      </EmptyState>
    );
  }

  const query = filter.trim().toLowerCase();
  const matches = repos.data.filter((r) => r.fullName.toLowerCase().includes(query));

  return (
    <div className={styles.picker}>
      <SearchInput
        value={filter}
        onChange={(e) => setFilter(e.target.value)}
        placeholder="Search repositories"
        autoFocus
      />
      <ul className={styles.repos}>
        {matches.map((r) => (
          <li key={r.fullName}>
            <button type="button" className={styles.repo} onClick={() => onPick(r)}>
              <span className={styles.repoName}>{r.fullName}</span>
              {r.private && <Lock size={12} aria-label="Private" />}
            </button>
          </li>
        ))}
        {matches.length === 0 && (
          <li className={styles.noMatch}>No repositories match “{filter}”.</li>
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

function DatabaseStep(props: StepProps) {
  const [kind, setKind] = useState<ServiceKind>("postgres");
  const name = useServiceName(kind);
  return (
    <StepForm
      {...props}
      canSubmit={!!name.final}
      onSubmit={() => props.onSubmit({ kind, name: name.final })}
    >
      <fieldset className={styles.kinds}>
        <legend className={styles.legend}>Engine</legend>
        {databaseKinds.map((k) => (
          <label key={k} className={styles.kind}>
            <input
              type="radio"
              name="kind"
              value={k}
              checked={kind === k}
              onChange={() => setKind(k)}
            />
            <Database size={14} />
            {kindLabels[k]}
          </label>
        ))}
      </fieldset>
      <NameField value={name.value} onChange={name.set} />
    </StepForm>
  );
}

function ImageStep(props: StepProps) {
  const [image, setImage] = useState("");
  const name = useServiceName(imageName(image));
  const ref = image.trim();
  return (
    <StepForm
      {...props}
      canSubmit={!!ref && !!name.final}
      onSubmit={() => props.onSubmit({ kind: "app", name: name.final, image: ref })}
    >
      <Field
        label="Image"
        hint="Any image reference, e.g. nginx:alpine or ghcr.io/owner/app:latest."
      >
        <Input
          mono
          value={image}
          onChange={(e) => setImage(e.target.value)}
          placeholder="nginx:alpine"
          autoFocus
        />
      </Field>
      <NameField value={name.value} onChange={name.set} />
    </StepForm>
  );
}
