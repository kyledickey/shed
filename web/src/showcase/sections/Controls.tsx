import {
  Activity,
  ChartArea,
  GitBranch,
  KeyRound,
  Plus,
  Rocket,
  ScrollText,
  Settings,
  Trash,
} from "lucide-react";
import { useState } from "react";
import type { DeploymentStatus, ServiceStatus } from "../../api/types";
import { Badge, StatusBadge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Field, Input, Segmented, Select, Switch, Textarea } from "../../components/Form";
import { Avatar, GitHubIcon, Kbd } from "../../components/Misc";
import { Tabs } from "../../components/Tabs";
import { crayons } from "../../components/tone";
import { Grid, Section, Specimen } from "../Section";
import s from "../showcase.module.css";

const deploymentStatuses: DeploymentStatus[] = [
  "queued",
  "waiting",
  "building",
  "deploying",
  "active",
  "failed",
  "crashed",
  "removed",
  "canceled",
  "skipped",
];
const serviceStatuses: ServiceStatus[] = ["active", "deploying", "offline", "failed", "crashed"];

export function Controls() {
  const [tab, setTab] = useState("deployments");
  const [auto, setAuto] = useState(true);
  const [ci, setCi] = useState(false);
  const [branch, setBranch] = useState("main");
  const [range, setRange] = useState("24h");
  const [loading, setLoading] = useState(false);

  const tabs = [
    { value: "deployments", label: "Deployments", icon: <Rocket size={15} />, count: 12 },
    { value: "logs", label: "Logs", icon: <ScrollText size={15} /> },
    { value: "metrics", label: "Metrics", icon: <ChartArea size={15} /> },
    { value: "variables", label: "Variables", icon: <KeyRound size={15} />, count: 8 },
    { value: "settings", label: "Settings", icon: <Settings size={15} /> },
  ];

  return (
    <>
      <Section title="Buttons" description="One primary per view. Soft buttons carry a crayon.">
        <Grid min={340}>
          <Specimen label="Variants">
            <div className={s.row}>
              <Button variant="primary">
                <Rocket size={14} />
                Deploy
              </Button>
              <Button>Redeploy</Button>
              <Button variant="ghost">Cancel</Button>
              <Button variant="soft">
                <Plus size={14} />
                New
              </Button>
              <Button variant="danger">
                <Trash size={14} />
                Delete
              </Button>
            </div>
          </Specimen>
          <Specimen label="Sizes & states">
            <div className={s.row}>
              <Button size="sm">Small</Button>
              <Button>Medium</Button>
              <Button size="lg" variant="primary">
                Large
              </Button>
              <Button
                loading={loading}
                onClick={() => {
                  setLoading(true);
                  setTimeout(() => setLoading(false), 1600);
                }}
              >
                {loading ? "Deploying" : "Click me"}
              </Button>
              <Button disabled>Disabled</Button>
            </div>
          </Specimen>
          <Specimen label="Icon buttons">
            <div className={s.row}>
              <Button icon aria-label="Settings">
                <Settings size={15} />
              </Button>
              <Button icon variant="ghost" aria-label="Activity">
                <Activity size={15} />
              </Button>
              <Button icon variant="soft" aria-label="Add">
                <Plus size={15} />
              </Button>
              <Button icon variant="primary" aria-label="Deploy">
                <Rocket size={15} />
              </Button>
              <Button size="sm" icon variant="ghost" aria-label="Delete">
                <Trash size={13} />
              </Button>
            </div>
          </Specimen>
          <Specimen label="Soft crayons">
            <div className={s.row}>
              {crayons.map((c) => (
                <Button key={c} variant="soft" tone={c} size="sm">
                  {c}
                </Button>
              ))}
            </div>
          </Specimen>
        </Grid>
      </Section>

      <Section
        title="Tabs"
        description="Pill tabs for the shell bar, line tabs for in-page sections."
      >
        <Specimen label="Pill">
          <div
            style={{
              padding: 6,
              background: "var(--shell)",
              borderRadius: "var(--r-lg)",
              border: "1px solid var(--line)",
            }}
          >
            <Tabs label="Service" items={tabs} value={tab} onChange={setTab} />
          </div>
        </Specimen>
        <Specimen label="Line">
          <Tabs label="Service" variant="line" items={tabs} value={tab} onChange={setTab} />
        </Specimen>
      </Section>

      <Section title="Inputs">
        <Grid min={320}>
          <Specimen label="Fields">
            <div className={s.stack}>
              <Field label="Service name" hint="Lowercase letters, numbers and dashes.">
                {(id) => <Input id={id} defaultValue="api" />}
              </Field>
              <Field label="Custom domain">
                {(id) => <Input id={id} prefix="https://" suffix=".acme.dev" placeholder="api" />}
              </Field>
              <Field
                label="Start command"
                error="Command can't be empty when there's no Dockerfile."
              >
                {(id) => <Input id={id} mono placeholder="bun run start" />}
              </Field>
            </div>
          </Specimen>
          <Specimen label="Pickers">
            <div className={s.stack}>
              <Field label="Branch">
                {(id) => (
                  <Select
                    id={id}
                    value={branch}
                    onChange={setBranch}
                    mono
                    options={["main", "staging", "feat/rate-limits", "fix/webhooks"].map((b) => ({
                      value: b,
                      label: b,
                    }))}
                  />
                )}
              </Field>
              <Field label="Range">
                {() => (
                  <Segmented
                    label="Range"
                    value={range}
                    onChange={setRange}
                    options={["1h", "6h", "24h", "7d"].map((r) => ({ value: r, label: r }))}
                  />
                )}
              </Field>
              <Field label="Raw .env">
                {(id) => (
                  <Textarea id={id} mono rows={3} defaultValue={"PORT=3000\nNODE_ENV=production"} />
                )}
              </Field>
            </div>
          </Specimen>
          <Specimen label="Switches">
            <div className={s.stack}>
              <Switch
                checked={auto}
                onChange={setAuto}
                label="Deploy on push"
                description="Every push to the tracked branch starts a deployment."
              />
              <Switch
                checked={ci}
                onChange={setCi}
                label="Wait for CI"
                description="Hold deployments until GitHub checks pass."
              />
              <Switch
                checked={false}
                onChange={() => {}}
                label="Serverless sleep"
                description="Coming later."
                disabled
              />
            </div>
          </Specimen>
        </Grid>
      </Section>

      <Section title="Badges & bits">
        <Grid min={340}>
          <Specimen label="Deployment status">
            <div className={s.row}>
              {deploymentStatuses.map((st) => (
                <StatusBadge key={st} kind="deployment" status={st} />
              ))}
            </div>
          </Specimen>
          <Specimen label="Service status">
            <div className={s.row}>
              {serviceStatuses.map((st) => (
                <StatusBadge key={st} kind="service" status={st} />
              ))}
            </div>
          </Specimen>
          <Specimen label="Soft · outline · solid">
            <div className={s.stack}>
              <div className={s.row}>
                {crayons.map((c) => (
                  <Badge key={c} tone={c}>
                    {c}
                  </Badge>
                ))}
              </div>
              <div className={s.row}>
                <Badge tone="sky" variant="outline" icon={<GitBranch size={11} />} mono>
                  main
                </Badge>
                <Badge tone="grass" variant="solid">
                  Serving
                </Badge>
                <Badge tone="neutral" mono>
                  4f2a9c1
                </Badge>
                <Badge tone="grape" variant="outline" icon={<GitHubIcon size={11} />}>
                  acme/api
                </Badge>
              </div>
            </div>
          </Specimen>
          <Specimen label="Avatars & keys">
            <div className={s.row}>
              <Avatar name="kyle" size={32} />
              <Avatar name="mira" size={28} />
              <Avatar name="sam" size={22} />
              <span style={{ width: 12 }} />
              <Kbd>⌘</Kbd>
              <Kbd>K</Kbd>
              <Kbd>esc</Kbd>
              <Kbd>↵</Kbd>
            </div>
          </Specimen>
        </Grid>
      </Section>
    </>
  );
}
