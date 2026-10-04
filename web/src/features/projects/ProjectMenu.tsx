import { useNavigate } from "@tanstack/react-router";
import { Ellipsis, Pencil, Trash } from "lucide-react";
import { useState } from "react";
import { useDeleteProject, useRenameProject } from "../../api/projects";
import type { Project } from "../../api/types";
import { Button } from "../../components/Button";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Menu, MenuItem, MenuSeparator } from "../../components/Menu";
import { ProjectNameDialog } from "./ProjectNameDialog";

export function ProjectMenu({ project }: { project: Project }) {
  const [dialog, setDialog] = useState<"rename" | "delete" | null>(null);
  const rename = useRenameProject(project.id);
  const remove = useDeleteProject(project.id);
  const navigate = useNavigate();

  const close = () => {
    setDialog(null);
    rename.reset();
    remove.reset();
  };

  return (
    <>
      <Menu
        trigger={
          <Button variant="ghost" icon aria-label="Project settings">
            <Ellipsis size={16} />
          </Button>
        }
      >
        <MenuItem icon={<Pencil size={14} />} onClick={() => setDialog("rename")}>
          Rename project
        </MenuItem>
        <MenuSeparator />
        <MenuItem icon={<Trash size={14} />} danger onClick={() => setDialog("delete")}>
          Delete project
        </MenuItem>
      </Menu>
      <ProjectNameDialog
        open={dialog === "rename"}
        onOpenChange={(open) => !open && close()}
        title="Rename project"
        submitLabel="Rename"
        initialName={project.name}
        pending={rename.isPending}
        error={rename.error}
        onSubmit={(name) => rename.mutate(name, { onSuccess: close })}
      />
      <ConfirmDialog
        open={dialog === "delete"}
        onOpenChange={(open) => !open && close()}
        title="Delete project"
        confirmLabel="Delete project"
        typeToConfirm={project.name}
        pending={remove.isPending}
        error={remove.error?.message}
        onConfirm={() => remove.mutate(undefined, { onSuccess: () => navigate({ to: "/" }) })}
      >
        <p>
          This stops and removes every service in <strong>{project.name}</strong>, including their
          volumes and data. This can't be undone.
        </p>
      </ConfirmDialog>
    </>
  );
}
