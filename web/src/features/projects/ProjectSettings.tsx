import { useNavigate } from "@tanstack/react-router";
import { CircleX, Trash, TriangleAlert } from "lucide-react";
import { useState } from "react";
import { errorMessage } from "../../api/client";
import { useDeleteProject, useRenameProject } from "../../api/projects";
import type { ProjectDetail } from "../../api/types";
import { Button } from "../../components/Button";
import { LayerCard } from "../../components/Card";
import { Field, Input } from "../../components/Form";
import { Callout } from "../../components/Misc";
import { Dialog, useToast } from "../../components/Overlay";
import styles from "./ProjectSettings.module.css";

/** RenameCard edits the project's name. */
export function RenameCard({ project }: { project: ProjectDetail }) {
  const [name, setName] = useState(project.name);
  const rename = useRenameProject(project.id);
  const toast = useToast();
  const trimmed = name.trim();

  return (
    <LayerCard title="General" padded>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!trimmed || trimmed === project.name) return;
          rename.mutate(trimmed, {
            onSuccess: (p) => {
              setName(p.name);
              toast.add({ title: "Project renamed", description: p.name, type: "success" });
            },
          });
        }}
      >
        <Field label="Project name" error={rename.error && errorMessage(rename.error)}>
          {(id) => (
            <div className={styles.row}>
              <div className={styles.grow}>
                <Input
                  id={id}
                  value={name}
                  autoComplete="off"
                  onChange={(e) => {
                    setName(e.target.value);
                    rename.reset();
                  }}
                />
              </div>
              <Button
                type="submit"
                variant="primary"
                loading={rename.isPending}
                disabled={!trimmed || trimmed === project.name}
              >
                Save
              </Button>
            </div>
          )}
        </Field>
      </form>
    </LayerCard>
  );
}

/** DangerCard deletes the project after the user types its name. */
export function DangerCard({ project }: { project: ProjectDetail }) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState("");
  const remove = useDeleteProject(project.id);
  const navigate = useNavigate();
  const toast = useToast();

  const change = (next: boolean) => {
    if (!next) {
      setTyped("");
      remove.reset();
    }
    setOpen(next);
  };

  return (
    <LayerCard
      title="Danger zone"
      icon={<TriangleAlert size={14} />}
      className={styles.danger}
      padded
    >
      <div className={styles.dangerRow}>
        <div className={styles.grow}>
          <div className={styles.dangerTitle}>Delete project</div>
          <p className={styles.dangerText}>
            Removes every service in this project with its containers, volumes and data.
          </p>
        </div>
        <Button variant="danger" onClick={() => change(true)}>
          <Trash size={14} />
          Delete project
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={change}
        title={`Delete ${project.name}?`}
        description="This stops and removes every service in the project, including their containers, volumes and data. This can't be undone."
        icon={<TriangleAlert size={18} />}
        iconTone="tomato"
        footer={
          <>
            <Button variant="ghost" onClick={() => change(false)}>
              Cancel
            </Button>
            <Button
              variant="danger"
              disabled={typed !== project.name}
              loading={remove.isPending}
              onClick={() =>
                remove.mutate(undefined, {
                  onSuccess: () => {
                    toast.add({
                      title: "Project deleted",
                      description: project.name,
                      type: "success",
                    });
                    void navigate({ to: "/" });
                  },
                })
              }
            >
              Delete project
            </Button>
          </>
        }
      >
        <Field
          label={
            <>
              Type <code className={styles.code}>{project.name}</code> to confirm
            </>
          }
        >
          {(id) => (
            <Input
              id={id}
              mono
              value={typed}
              autoComplete="off"
              autoFocus
              onChange={(e) => setTyped(e.target.value)}
            />
          )}
        </Field>
        {remove.error && (
          <Callout tone="tomato" icon={<CircleX size={16} />} title="Couldn't delete project">
            {errorMessage(remove.error)}
          </Callout>
        )}
      </Dialog>
    </LayerCard>
  );
}
