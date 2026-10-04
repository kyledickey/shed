import {
  Box,
  Command as CommandIcon,
  Copy,
  Database,
  Ellipsis,
  Leaf,
  Pencil,
  Rocket,
  Trash,
  TriangleAlert,
  Zap,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { Button } from "../../components/Button";
import { Field, Input } from "../../components/Form";
import { GitHubIcon, Kbd, ServiceIcon } from "../../components/Misc";
import {
  Dialog,
  Menu,
  MenuItem,
  MenuLabel,
  MenuSeparator,
  Tooltip,
  useToast,
} from "../../components/Overlay";
import type { Tone } from "../../components/tone";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

const kinds: { key: string; name: string; hint: string; icon: ReactNode; tone: Tone }[] = [
  {
    key: "repo",
    name: "GitHub repo",
    hint: "Build & deploy on push",
    icon: <GitHubIcon />,
    tone: "grape",
  },
  { key: "image", name: "Docker image", hint: "Any public image", icon: <Box />, tone: "teal" },
  { key: "postgres", name: "Postgres", hint: "v17 with a volume", icon: <Database />, tone: "sky" },
  { key: "mysql", name: "MySQL", hint: "v8.4", icon: <Database />, tone: "tangerine" },
  { key: "mongo", name: "MongoDB", hint: "v8", icon: <Leaf />, tone: "grass" },
  { key: "redis", name: "Redis", hint: "v7, persistent", icon: <Zap />, tone: "tomato" },
];

export function Overlays({ onCommand }: { onCommand: () => void }) {
  const [create, setCreate] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [kind, setKind] = useState("repo");
  const [typed, setTyped] = useState("");
  const toast = useToast();

  return (
    <Section
      title="Overlays"
      description="Dialogs spring in, menus fade from their trigger, toasts stack."
    >
      <Grid min={300}>
        <Specimen label="Dialog">
          <div className={s.row}>
            <Button variant="primary" onClick={() => setCreate(true)}>
              New service
            </Button>
            <Button variant="danger" onClick={() => setConfirm(true)}>
              Delete service
            </Button>
          </div>
        </Specimen>
        <Specimen label="Menu & tooltip">
          <div className={s.row}>
            <Menu
              align="start"
              trigger={
                <Button>
                  <Ellipsis size={14} /> Actions
                </Button>
              }
            >
              <MenuLabel>Service</MenuLabel>
              <MenuItem icon={<Rocket size={14} />} shortcut="⌘D">
                Deploy
              </MenuItem>
              <MenuItem icon={<Pencil size={14} />}>Rename</MenuItem>
              <MenuItem icon={<Copy size={14} />}>Copy private host</MenuItem>
              <MenuSeparator />
              <MenuItem icon={<Trash size={14} />} danger>
                Delete
              </MenuItem>
            </Menu>
            <Tooltip content="Redeploys the latest commit">
              <Button variant="ghost">Hover me</Button>
            </Tooltip>
          </div>
        </Specimen>
        <Specimen label="Toasts">
          <div className={s.row}>
            <Button
              size="sm"
              variant="soft"
              tone="grass"
              onClick={() =>
                toast.add({
                  title: "Deployed",
                  description: "api is live at api.acme.dev",
                  type: "success",
                })
              }
            >
              Success
            </Button>
            <Button
              size="sm"
              variant="soft"
              tone="tomato"
              onClick={() =>
                toast.add({
                  title: "Build failed",
                  description: "npm ci exited with code 1",
                  type: "error",
                })
              }
            >
              Error
            </Button>
            <Button
              size="sm"
              variant="soft"
              tone="sky"
              onClick={() =>
                toast.add({
                  title: "Copied",
                  description: "DATABASE_URL copied to clipboard",
                  type: "info",
                })
              }
            >
              Info
            </Button>
          </div>
        </Specimen>
        <Specimen label="Command palette">
          <div className={s.row}>
            <Button onClick={onCommand}>
              <CommandIcon size={14} /> Open palette
            </Button>
            <span className={s.note}>
              or press <Kbd>⌘</Kbd> <Kbd>K</Kbd>
            </span>
          </div>
        </Specimen>
      </Grid>

      <Dialog
        open={create}
        onOpenChange={setCreate}
        size="md"
        title="New service"
        description="Add an app or a database to acme."
        footer={
          <>
            <Button variant="ghost" onClick={() => setCreate(false)}>
              Cancel
            </Button>
            <Button variant="primary" onClick={() => setCreate(false)}>
              Create service
            </Button>
          </>
        }
      >
        <div className={s.kindGrid}>
          {kinds.map((k) => (
            <button
              key={k.key}
              type="button"
              className={s.kindTile}
              data-tone={k.tone}
              aria-pressed={kind === k.key}
              onClick={() => setKind(k.key)}
            >
              <ServiceIcon icon={k.icon} tone={k.tone} size={30} />
              <span>
                <div className={s.kindName}>{k.name}</div>
                <div className={s.kindHint}>{k.hint}</div>
              </span>
            </button>
          ))}
        </div>
        <Field label="Name">
          {(id) => <Input id={id} defaultValue={kind === "repo" ? "api" : kind} />}
        </Field>
      </Dialog>

      <Dialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Delete api?"
        description="This removes the container, its domains, and its deployment history. Volumes are kept."
        icon={<TriangleAlert size={18} />}
        iconTone="tomato"
        footer={
          <>
            <Button variant="ghost" onClick={() => setConfirm(false)}>
              Cancel
            </Button>
            <Button variant="danger" disabled={typed !== "api"} onClick={() => setConfirm(false)}>
              Delete service
            </Button>
          </>
        }
      >
        <Field
          label={
            <>
              Type <code>api</code> to confirm
            </>
          }
        >
          {(id) => <Input id={id} mono value={typed} onChange={(e) => setTyped(e.target.value)} />}
        </Field>
      </Dialog>
    </Section>
  );
}
