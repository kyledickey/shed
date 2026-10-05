import { useEffect, useState } from "react";
import { Segmented } from "../../../../components/Form";
import { Diagram, Edge, Node, Note } from "../../diagram";
import { Demo, DocTable, Figure } from "../../kit";
import { BUCKETS, bucketWindow, rangeSeconds, type BucketRange } from "../../lib/buckets";
import styles from "./metrics.module.css";

const ranges = (Object.keys(rangeSeconds) as BucketRange[]).map((r) => ({ value: r, label: r }));

const hms = (sec: number) => new Date(sec * 1000).toISOString().slice(11, 19);
const stamp = (sec: number) => new Date(sec * 1000).toISOString().slice(0, 19).replace("T", " ");

/** A fixed instant (2026-01-01 00:12:17 UTC) for the first render, so prerendered HTML hydrates. */
const sampleNow = 1_767_226_337;

/** BucketExplorer shows the window a metrics query covers. */
export function BucketExplorer() {
  const [range, setRange] = useState<BucketRange>("1h");
  const [hasSamples, setHasSamples] = useState(true);
  const [now, setNow] = useState(sampleNow);
  useEffect(() => setNow(Math.floor(Date.now() / 1000)), []);
  const w = bucketWindow(now, range, hasSamples);
  return (
    <Demo title="The window of a query">
      <div className={styles.toolbar}>
        <Segmented label="Range" size="sm" value={range} onChange={setRange} options={ranges} />
        <Segmented
          label="Bucket containing now"
          size="sm"
          value={hasSamples ? "samples" : "empty"}
          onChange={(v) => setHasSamples(v === "samples")}
          options={[
            { value: "samples", label: "Has samples" },
            { value: "empty", label: "Still empty" },
          ]}
        />
      </div>
      <DocTable
        mono
        head={["Field", "Value"]}
        rows={[
          ["now", `${stamp(now)} UTC`],
          ["step", `${w.step}s`],
          ["start", `${stamp(w.start)} UTC`],
          ["end", `${stamp(w.end)} UTC`],
          ["start / step", `${w.start / w.step} (a whole number: epoch-aligned)`],
        ]}
      />
      <p className={styles.verdict}>
        {w.shifted
          ? "The bucket containing now is still empty, so the window shifts back one step and ends at the last complete bucket."
          : "The bucket containing now already has samples, so the window ends with it."}
      </p>
      <Figure caption="The last four buckets of the window, drawn at the instant this page loaded.">
        <BucketStrip range={range} now={now} shifted={w.shifted} />
      </Figure>
    </Demo>
  );
}

function BucketStrip({
  range,
  now,
  shifted,
}: {
  range: BucketRange;
  now: number;
  shifted: boolean;
}) {
  const step = rangeSeconds[range] / BUCKETS;
  const end = (Math.floor(now / step) + 1) * step;
  const xNow = 40 + 3 * 180 + ((now - (end - step)) / step) * 170;
  const xEnd = shifted ? 40 + 2 * 180 + 170 : 40 + 3 * 180 + 170;
  return (
    <Diagram width={800} height={150} label="The last buckets of a window and where it ends">
      {[0, 1, 2, 3].map((i) => {
        const young = i === 3;
        return (
          <Node
            key={i}
            x={40 + i * 180}
            y={40}
            w={170}
            title={hms(end - (4 - i) * step)}
            sub={young ? (shifted ? "contains now, empty" : "contains now") : `${step}s, complete`}
            tone={young ? "accent" : "neutral"}
            dim={young && shifted}
            emphasis={young && !shifted}
          />
        );
      })}
      <Note x={xNow} y={14} anchor="middle" mono>
        {`now ${hms(now)}`}
      </Note>
      <Edge
        points={[
          [xNow, 20],
          [xNow, 40],
        ]}
        tone="accent"
      />
      <Edge
        points={[
          [xEnd, 128],
          [xEnd, 92],
        ]}
      />
      <Note x={xEnd} y={144} anchor="end">
        window ends here
      </Note>
    </Diagram>
  );
}
