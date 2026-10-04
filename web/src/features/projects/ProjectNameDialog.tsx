import { useState } from "react";
import { Button } from "../../components/Button";
import { Dialog, DialogBody, DialogFooter, DialogForm } from "../../components/Dialog";
import { Field } from "../../components/Field";
import { Input } from "../../components/Input";

type ProjectNameDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  submitLabel: string;
  initialName?: string;
  pending: boolean;
  error: Error | null;
  onSubmit: (name: string) => void;
};

/** Create or rename a project. */
export function ProjectNameDialog({ open, onOpenChange, title, ...rest }: ProjectNameDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={title}>
      <NameForm onCancel={() => onOpenChange(false)} {...rest} />
    </Dialog>
  );
}

function NameForm({
  submitLabel,
  initialName = "",
  pending,
  error,
  onSubmit,
  onCancel,
}: Omit<ProjectNameDialogProps, "open" | "onOpenChange" | "title"> & { onCancel: () => void }) {
  const [name, setName] = useState(initialName);
  const trimmed = name.trim();
  return (
    <DialogForm onSubmit={() => trimmed && onSubmit(trimmed)}>
      <DialogBody>
        <Field label="Name" error={error?.message}>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="my-project"
            autoFocus
          />
        </Field>
      </DialogBody>
      <DialogFooter>
        <Button onClick={onCancel}>Cancel</Button>
        <Button
          type="submit"
          variant="primary"
          loading={pending}
          disabled={!trimmed || trimmed === initialName}
        >
          {submitLabel}
        </Button>
      </DialogFooter>
    </DialogForm>
  );
}
