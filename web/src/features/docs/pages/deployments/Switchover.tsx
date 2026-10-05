import { useState } from "react";
import { Button } from "../../../../components/Button";
import { Segmented } from "../../../../components/Form";
import { cx } from "../../../../lib/cx";
import { Diagram, Edge, Node, Note, Zone } from "../../diagram";
import { Demo } from "../../kit";
import {
  switchoverFrames,
  type ContainerView,
  type Frame,
  type SwitchMode,
} from "../../lib/switchover";
import styles from "../deployments.module.css";

/** Switchover is a stepper over the ordering of a deployment going live. */
export function Switchover() {
  return (
    <Demo title="Step through a switchover">
      <Stepper name="web" port={3000} />
    </Demo>
  );
}

function Stepper({ name, port }: { name: string; port: number }) {
  const [mode, setMode] = useState<SwitchMode>("overlap");
  const [step, setStep] = useState(0);
  const frames = switchoverFrames(mode);
  const at = Math.min(step, frames.length - 1);
  const frame = frames[at]!;

  return (
    <div className={styles.stepper}>
      <Segmented
        label="Replacement mode"
        size="sm"
        value={mode}
        onChange={setMode}
        options={[
          { value: "overlap", label: "Overlap", title: "No volumes and no public port" },
          { value: "stop-first", label: "Stop first", title: "Volumes or a public port" },
        ]}
      />
      <SwitchDiagram frame={frame} name={name} port={port} />
      <ol className={styles.stepList}>
        {frames.map((fr, i) => (
          <li key={fr.title}>
            <button
              type="button"
              className={cx(styles.stepButton, i === at && styles.stepHere)}
              aria-current={i === at ? "step" : undefined}
              onClick={() => setStep(i)}
            >
              <span className={styles.stepNo}>{i + 1}</span>
              {fr.title}
            </button>
          </li>
        ))}
      </ol>
      <p className={styles.frameDetail}>{frame.detail}</p>
      <div className={styles.stepNav}>
        <Button size="sm" onClick={() => setStep(Math.max(0, at - 1))} disabled={at === 0}>
          Previous
        </Button>
        <Button
          size="sm"
          variant="primary"
          onClick={() => setStep(Math.min(frames.length - 1, at + 1))}
          disabled={at === frames.length - 1}
        >
          Next
        </Button>
      </div>
    </div>
  );
}

function describe(c: ContainerView, absent: string): string {
  if (c.state === "absent") return absent;
  return `${c.state} · ${c.alias ? "has alias" : "no alias"}`;
}

function SwitchDiagram({ frame, name, port }: { frame: Frame; name: string; port: number }) {
  const label = `${name}:${port}`;
  const oldPath: [number, number][] = [
    [130, 142],
    [200, 142],
    [200, 78],
    [280, 78],
  ];
  const newPath: [number, number][] = [
    [130, 142],
    [200, 142],
    [200, 218],
    [280, 218],
  ];
  return (
    <Diagram
      width={780}
      height={290}
      label="Which container receives public and private traffic at this step"
    >
      <Zone x={260} y={10} w={280} h={270} label="network shed-<projectID>" tone="sky" />
      <Node
        x={10}
        y={114}
        w={120}
        h={56}
        title="Caddy"
        sub="public routes"
        tone="accent"
        emphasis
      />
      <Node
        x={280}
        y={46}
        w={240}
        h={64}
        title="Old container"
        sub={describe(frame.old, "removed")}
        tone={frame.routes === "old" ? "accent" : "neutral"}
        emphasis={frame.routes === "old"}
        dim={frame.old.state === "absent"}
      />
      <Node
        x={280}
        y={186}
        w={240}
        h={64}
        title="New container"
        sub={describe(frame.next, "not created yet")}
        tone={frame.routes === "new" ? "accent" : "neutral"}
        emphasis={frame.routes === "new"}
        dim={frame.next.state === "absent"}
      />
      <Node x={590} y={114} w={180} h={56} title="Other services" sub={label} />
      {frame.routes === "old" && <Edge points={oldPath} flow tone="accent" />}
      {frame.routes === "new" && <Edge points={newPath} flow tone="accent" />}
      {frame.routes === "none" && (
        <Note x={10} y={200}>
          No live upstream: requests fail
        </Note>
      )}
      {frame.old.alias && frame.old.state === "running" && (
        <Edge
          points={[
            [590, 142],
            [565, 142],
            [565, 78],
            [520, 78],
          ]}
          dashed
        />
      )}
      {frame.next.alias && frame.next.state === "running" && (
        <Edge
          points={[
            [590, 142],
            [565, 142],
            [565, 218],
            [520, 218],
          ]}
          dashed
        />
      )}
    </Diagram>
  );
}
