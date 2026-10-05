import { useNavigate } from "@tanstack/react-router";
import { FolderPlus } from "lucide-react";
import { useId, useState } from "react";
import { errorMessage } from "../../api/client";
import { useCreateProject } from "../../api/projects";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { Dialog, useToast } from "../../components/Overlay";

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
    <Dialog
      open={open}
      onOpenChange={change}
      title="New project"
      description="A project groups services that reach each other over a private network."
      icon={<FolderPlus size={18} />}
      footer={
        <>
          <Button variant="ghost" onClick={() => change(false)}>
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
        <Field label="Name" error={create.error && errorMessage(create.error)}>
          {(id) => (
            <Input
              id={id}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-project"
              autoComplete="off"
              autoFocus
            />
          )}
        </Field>
      </form>
    </Dialog>
  );
}
