import { Button, Input } from "@cloudflare/kumo";
import { useNavigate } from "@tanstack/react-router";
import { useId, useState } from "react";
import { errorMessage } from "../../api/client";
import { useCreateProject } from "../../api/projects";
import { useToast } from "../../components/Overlay";
import { FormDialog } from "../settings/FormDialog";
import styles from "./ProjectList.module.css";

type NewProjectDialogProps = { open: boolean; onOpenChange: (open: boolean) => void };

/** NewProjectDialog names a new project, creates it and opens it. */
export function NewProjectDialog({ open, onOpenChange }: NewProjectDialogProps) {
  const formId = useId();
  const [name, setName] = useState("");
  const create = useCreateProject();
  const navigate = useNavigate();
  const toast = useToast();
  const trimmed = name.trim();

  const change = (next: boolean) => {
    if (!next) {
      setName("");
      create.reset();
    }
    onOpenChange(next);
  };

  return (
    <FormDialog
      open={open}
      onOpenChange={change}
      size="base"
      title="New project"
      description="A project groups services that reach each other over a private network. Add an app or a database once it exists."
      footer={
        <>
          <Button variant="secondary" onClick={() => change(false)}>
            Cancel
          </Button>
          <Button
            type="submit"
            form={formId}
            variant="primary"
            loading={create.isPending}
            disabled={!trimmed}
          >
            Create project
          </Button>
        </>
      }
    >
      <form
        id={formId}
        className={styles.form}
        onSubmit={(e) => {
          e.preventDefault();
          if (!trimmed) return;
          create.mutate(trimmed, {
            onSuccess: (project) => {
              toast.add({ title: "Project created", description: project.name, type: "success" });
              void navigate({ to: "/projects/$projectId", params: { projectId: project.id } });
            },
          });
        }}
      >
        <Input
          label="Project name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="my-project"
          autoComplete="off"
          autoFocus
          error={create.error ? errorMessage(create.error) : undefined}
        />
      </form>
    </FormDialog>
  );
}
