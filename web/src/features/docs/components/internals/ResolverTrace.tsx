import { ChevronLeft, ChevronRight, RotateCcw } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "../../../../components/Button";
import { Segmented, Textarea } from "../../../../components/Form";
import { cx } from "../../../../lib/cx";
import { Demo } from "../../kit";
import styles from "./internals.module.css";
import { parseScope, traceResolve } from "./trace";

const samples = {
  works: `web.URL=postgres://\${{ db.USER }}@\${{ db.HOST }}/\${{ NAME }}
web.NAME=app
db.USER=admin
db.HOST=\${{ SHED_PRIVATE_DOMAIN }}
db.SHED_PRIVATE_DOMAIN=db`,
  cycle: `web.A=\${{ B }}
web.B=\${{ C }}
web.C=\${{ A }}`,
} as const;

type Sample = keyof typeof samples;

/** ResolverTrace steps through internal/vars.Resolve for service web, on text the reader can edit. */
export function ResolverTrace() {
  const [sample, setSample] = useState<Sample>("works");
  const [text, setText] = useState<string>(samples.works);
  const [at, setAt] = useState(1);
  const steps = useMemo(() => traceResolve("web", parseScope(text)), [text]);
  const shown = Math.min(at, steps.length);
  const step = steps[shown - 1];

  const load = (next: Sample) => {
    setSample(next);
    setText(samples[next]);
    setAt(1);
  };

  return (
    <Demo
      title="Step through Resolve"
      actions={
        <Segmented
          label="Sample"
          size="sm"
          value={sample}
          onChange={load}
          options={[
            { value: "works", label: "References" },
            { value: "cycle", label: "Cycle" },
          ]}
        />
      }
    >
      <p className={styles.hint}>
        One variable per line as <code>service.KEY=value</code>. Resolve runs for the service{" "}
        <code>web</code>, visiting its keys in sorted order.
      </p>
      <Textarea
        mono
        rows={5}
        value={text}
        spellCheck={false}
        aria-label="Variables"
        onChange={(e) => {
          setText(e.target.value);
          setAt(1);
        }}
      />
      <div className={styles.stepBar}>
        <Button
          size="sm"
          icon
          aria-label="Previous step"
          disabled={shown <= 1}
          onClick={() => setAt(shown - 1)}
        >
          <ChevronLeft size={14} />
        </Button>
        <Button
          size="sm"
          icon
          aria-label="Next step"
          disabled={shown >= steps.length}
          onClick={() => setAt(shown + 1)}
        >
          <ChevronRight size={14} />
        </Button>
        <Button size="sm" icon aria-label="Restart" onClick={() => setAt(1)}>
          <RotateCcw size={14} />
        </Button>
        <span className={styles.stepCount}>
          {steps.length === 0 ? "no variables for web" : `step ${shown} of ${steps.length}`}
        </span>
      </div>
      {step && (
        <>
          <ol className={styles.steps}>
            {steps.slice(0, shown).map((s, i) => (
              <li key={i} className={cx(i === shown - 1 && styles.stepNow)}>
                <code className={styles.stepKind} data-kind={s.kind}>
                  {s.kind}
                </code>
                <code>{s.ref}</code>
                <span>{s.note}</span>
              </li>
            ))}
          </ol>
          <div className={styles.state}>
            <div>
              <div className={styles.stateLabel}>stack (visiting)</div>
              <div className={styles.stateBody}>
                {step.stack.length === 0 ? (
                  <span className={styles.none}>empty</span>
                ) : (
                  step.stack.map((r) => <code key={r}>{r}</code>)
                )}
              </div>
            </div>
            <div>
              <div className={styles.stateLabel}>memo (resolved)</div>
              <div className={styles.stateBody}>
                {step.memo.length === 0 ? (
                  <span className={styles.none}>empty</span>
                ) : (
                  step.memo.map(([r, v]) => (
                    <code key={r}>
                      {r} = {v}
                    </code>
                  ))
                )}
              </div>
            </div>
          </div>
        </>
      )}
    </Demo>
  );
}
