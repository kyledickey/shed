import { Button, Input } from "@cloudflare/kumo";
import { TrashIcon } from "@phosphor-icons/react";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useDeleteProject, useRenameProject } from "../../api/projects";
import type { ProjectDetail } from "../../api/types";
import { useToast } from "../../components/Overlay";
import { SettingRow } from "../../components/Settings";
import { cx } from "../../lib/cx";
import { ConfirmDialog } from "../settings/ConfirmDialog";
import styles from "../settings/Form.module.css";
import { SettingsForm } from "../settings/SectionForm";

/** GeneralSection edits the project's name. */
export function GeneralSection({ project }: { project: ProjectDetail }) {
  const [name, setName] = useState(project.name);
  const rename = useRenameProject(project.id);
  const toast = useToast();
  const trimmed = name.trim();

  return (
    <SettingsForm
      form={{
        dirty: trimmed !== project.name,
        invalid: !trimmed,
        message: trimmed ? undefined : "The name can't be empty.",
        pending: rename.isPending,
        error: rename.error,
        reset: () => {
          setName(project.name);
          rename.reset();
        },
        save: () =>
          rename.mutate(trimmed, {
            onSuccess: (p) => {
              setName(p.name);
              toast.add({ title: "Project renamed", description: p.name, type: "success" });
            },
          }),
      }}
    >
      <SettingRow
        label="Name"
        description="How the project appears across shed."
        control={
          <Input
            aria-label="Project name"
            className={cx(styles.wide)}
            value={name}
            autoComplete="off"
            onChange={(e) => {
              setName(e.target.value);
              rename.reset();
            }}
          />
        }
      />
    </SettingsForm>
  );
}

/** DangerSection deletes the project after the user types its name. */
export function DangerSection({ project }: { project: ProjectDetail }) {
  const [open, setOpen] = useState(false);
  const remove = useDeleteProject(project.id);
  const navigate = useNavigate();
  const toast = useToast();

  return (
    <SettingRow
      label="Delete project"
      description="Remove every service in this project with its containers, volumes, and data."
      control={
        <Button variant="destructive" icon={<TrashIcon />} onClick={() => setOpen(true)}>
          Delete
        </Button>
      }
    >
      <ConfirmDialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next);
          remove.reset();
        }}
        title={`Delete ${project.name}?`}
        confirmLabel="Delete project"
        typeToConfirm={project.name}
        pending={remove.isPending}
        error={remove.error ? errorMessage(remove.error) : undefined}
        onConfirm={() =>
          remove.mutate(undefined, {
            onSuccess: () => {
              toast.add({ title: "Project deleted", description: project.name, type: "success" });
              void navigate({ to: "/" });
            },
          })
        }
      >
        This stops and removes every service in the project, including their containers, volumes,
        and data. This can't be undone.
      </ConfirmDialog>
    </SettingRow>
  );
}
