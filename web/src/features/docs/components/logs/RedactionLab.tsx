import { useMemo, useState } from "react";
import { Badge } from "../../../../components/Badge";
import { Field, Input, Segmented, Textarea } from "../../../../components/Form";
import { Demo, DocTable } from "../../kit";
import { chunk, redactEach, StreamRedactor } from "../../lib/redact";
import styles from "./logs.module.css";

type Size = "4" | "8" | "16" | "all";

const SAMPLE = `connecting with password=hunter2
retry: auth failed for hunter2
token=hunter2 expires soon`;

const show = (s: string) => (s === "" ? "" : s.replaceAll("\n", "↵"));

/** RedactionLab masks text you type, entirely in the browser. */
export function RedactionLab() {
  const [secret, setSecret] = useState("hunter2");
  const [text, setText] = useState(SAMPLE);
  const [size, setSize] = useState<Size>("8");

  const run = useMemo(() => {
    const pieces = size === "all" ? [text] : chunk(text, Number(size));
    const redactor = new StreamRedactor([secret]);
    const rows = pieces.map((piece) => ({
      piece,
      naive: redactEach(piece, [secret]),
      emitted: redactor.write(piece),
      held: redactor.held,
    }));
    const flushed = redactor.flush();
    return { rows, flushed, tail: redactor.tail };
  }, [secret, text, size]);

  const naive = run.rows.map((r) => r.naive).join("");
  const streamed = run.rows.map((r) => r.emitted).join("") + run.flushed;
  const leaked = secret !== "" && naive.includes(secret);

  return (
    <Demo title="Try the redactor">
      <div className={styles.toolbar}>
        <div className={styles.field}>
          <Field
            label="Fake secret"
            hint="Use a made-up value. This runs in your browser and nothing is sent."
          >
            {(id) => (
              <Input id={id} mono value={secret} onChange={(e) => setSecret(e.target.value)} />
            )}
          </Field>
        </div>
        <Segmented
          label="Write size in characters"
          size="sm"
          value={size}
          onChange={setSize}
          options={[
            { value: "4", label: "4" },
            { value: "8", label: "8" },
            { value: "16", label: "16" },
            { value: "all", label: "One write" },
          ]}
        />
      </div>
      <Field label="Log output">
        {(id) => (
          <Textarea id={id} mono rows={4} value={text} onChange={(e) => setText(e.target.value)} />
        )}
      </Field>
      <p className={styles.muted}>
        The text arrives as {run.rows.length} {run.rows.length === 1 ? "write" : "writes"}. After
        each one the streaming redactor holds back the last {run.tail} characters, one fewer than
        the secret, because they could be the start of a match.
      </p>
      <DocTable
        head={["#", "Write", "Per-write replace", "Streaming emits", "Held back"]}
        rows={[
          ...run.rows.map((r, i) => [
            String(i + 1),
            <span key="w" className={styles.cell}>
              {show(r.piece)}
            </span>,
            <span key="n" className={styles.cell}>
              {show(r.naive)}
            </span>,
            <span key="e" className={styles.cell}>
              {show(r.emitted)}
            </span>,
            <span key="h" className={styles.cell}>
              {show(r.held)}
            </span>,
          ]),
          [
            "end",
            <span key="w" className={styles.muted}>
              flush
            </span>,
            "",
            <span key="e" className={styles.cell}>
              {show(run.flushed)}
            </span>,
            "",
          ],
        ]}
      />
      <div className={styles.outputs}>
        <div className={styles.output}>
          <span className={styles.outputTitle}>
            Replacing inside each write
            <Badge size="sm" tone={leaked ? "tomato" : "neutral"}>
              {leaked ? "secret leaked" : "no leak here"}
            </Badge>
          </span>
          <pre className={styles.outputBody}>{naive}</pre>
        </div>
        <div className={styles.output}>
          <span className={styles.outputTitle}>
            Streaming redactor
            <Badge size="sm" tone={secret === "" ? "neutral" : "grass"}>
              {secret === "" ? "no secret set" : "masked"}
            </Badge>
          </span>
          <pre className={styles.outputBody}>{streamed}</pre>
        </div>
      </div>
    </Demo>
  );
}
