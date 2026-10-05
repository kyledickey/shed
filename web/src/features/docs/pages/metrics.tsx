import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { Segmented } from "../../../components/Form";
import { Diagram, Edge, Node, Note } from "../diagram";
import { CodeBlock, Demo, Doc, DocSection, DocTable, Figure } from "../kit";
import { BUCKETS, bucketWindow, rangeSeconds, type BucketRange } from "../lib/buckets";
import styles from "./metrics.module.css";

const ranges = (Object.keys(rangeSeconds) as BucketRange[]).map((r) => ({ value: r, label: r }));

const hms = (sec: number) => new Date(sec * 1000).toISOString().slice(11, 19);
const stamp = (sec: number) => new Date(sec * 1000).toISOString().slice(0, 19).replace("T", " ");

export function MetricsDoc() {
  return (
    <Doc
      slug="metrics"
      lede="shed samples every running container and the host itself every 10 seconds, keeps a week of history in SQLite, and serves it as 180 evenly spaced buckets per chart."
    >
      <DocSection page="metrics" id="sampling">
        <p>
          A collector ticks every 10 seconds. It lists the running containers that carry the{" "}
          <code>shed.service</code> label and asks Docker for a one-shot <code>stats</code> reading
          of each. Those readings are cumulative counters, so a number only exists once there are
          two of them: shed keeps the previous reading per container and turns the difference into a
          rate. A container's first reading yields nothing, and neither does one where a counter
          went backwards, as after a restart.
        </p>
        <Figure caption="From Docker's counters to a chart. Host readings take the same path.">
          <SamplingDiagram />
        </Figure>
        <CodeBlock title="what one sample stores" lang="math">
          {`cpu     = Δcpu_ns / Δsystem_ns × online_cpus × 100   # percent of ONE core (fallback: Δcpu_ns / Δwall_ns × 100)
memory  = usage − inactive_file                      # a level, not a rate: page cache that can be dropped is not counted
net     = Δbytes / Δseconds                          # rx and tx, summed over all interfaces
disk    = Δbytes / Δseconds                          # block reads and writes`}
        </CodeBlock>
        <p>
          CPU is a percentage of <em>one</em> core, so a busy two-thread app can show 200%. A
          service's limit in the same units is <code>cpuLimit × 100</code>. During a zero-downtime
          deploy two containers of a service run side by side, and their rates are summed into one
          row per service per tick. Samples older than 7 days are deleted hourly.
        </p>
      </DocSection>

      <DocSection page="metrics" id="host">
        <p>
          The host is read on the same tick from the kernel, not from Docker, so it includes
          everything on the box, not just shed's containers.
        </p>
        <DocTable
          mono
          head={["Metric", "Source", "Computation"]}
          rows={[
            [
              "cpu",
              "/proc/stat",
              "Total is the first eight fields of the cpu line (user through steal). Busy is total minus idle and iowait, clamped to the elapsed total. Percent is busy / total × CPUs × 100.",
            ],
            ["memory", "/proc/meminfo", "MemTotal − MemAvailable."],
            [
              "net, disk I/O",
              "/sys/class/net, /proc/diskstats",
              "Bytes per second summed over physical devices only: those with a device link in sysfs. Docker bridges, veth pairs, loop and device-mapper devices are left out so traffic is not counted twice. Sectors are 512 bytes.",
            ],
            ["disk used", "statfs", "Used space of the filesystem that holds data.dir."],
          ]}
        />
        <p>
          <code>cpus</code>, <code>memoryTotal</code>, and <code>diskTotal</code> are read live when
          you query, not stored per sample.
        </p>
      </DocSection>

      <DocSection page="metrics" id="buckets">
        <p>
          A query asks for a range: <code>1h</code>, <code>6h</code>, <code>24h</code>, or{" "}
          <code>7d</code>. The answer always has <strong>{BUCKETS} buckets</strong>, each the
          average of the samples that fall in it, or <code>null</code> if there are none. The
          response gives <code>start</code> and <code>step</code>, and bucket <em>i</em> covers{" "}
          <code>[start + i × step, start + (i + 1) × step)</code>.
        </p>
        <DocTable
          mono
          head={["Range", "Bucket width", "Samples per bucket"]}
          rows={ranges.map((r) => [
            r.value,
            `${rangeSeconds[r.value] / BUCKETS}s`,
            String(rangeSeconds[r.value] / BUCKETS / 10),
          ])}
        />
        <p>
          Buckets are aligned to multiples of their width since the Unix epoch, so a bucket's
          boundaries are the same on every query and charts do not jitter as time passes. The window
          normally ends with the bucket that contains <em>now</em>. But a bucket that has just
          started may not have a sample yet, and a chart that ends in a hole looks like an outage.
          In that case the whole window shifts back one step, to end at the last complete bucket. To
          make that possible the server fetches {BUCKETS + 1} buckets, one before the window.
        </p>
        <BucketExplorer />
        <p>
          Charts refetch about once per bucket, between 10 seconds and a minute. The same bucketing
          applies to service metrics and host metrics. See{" "}
          <Link to="/docs/$slug" params={{ slug: "resources" }} hash="limits">
            CPU and memory limits
          </Link>{" "}
          for the limit lines drawn on service charts.
        </p>
      </DocSection>
    </Doc>
  );
}

function SamplingDiagram() {
  return (
    <Diagram width={800} height={250} label="How container and host samples become charts">
      <Node x={0} y={20} w={190} title="docker stats" sub="one-shot, per container" tone="sky" />
      <Node x={0} y={90} w={190} title="/proc, /sys, statfs" sub="host counters" tone="sky" />
      <Node
        x={250}
        y={55}
        w={160}
        h={70}
        title="Collector"
        sub="tick every 10s"
        tone="accent"
        emphasis
      />
      <Node x={470} y={20} w={150} title="rates" sub="Δ counters / Δ time" />
      <Node x={470} y={90} w={150} title="sum per service" sub="overlap on deploy" />
      <Node x={660} y={55} w={140} h={70} title="SQLite" sub="7 days kept" />
      <Edge
        points={[
          [190, 46],
          [220, 46],
          [220, 80],
          [250, 80],
        ]}
      />
      <Edge
        points={[
          [190, 116],
          [220, 116],
          [220, 100],
          [250, 100],
        ]}
      />
      <Edge
        points={[
          [410, 90],
          [440, 90],
          [440, 46],
          [470, 46],
        ]}
      />
      <Edge
        points={[
          [545, 72],
          [545, 90],
        ]}
      />
      <Edge
        points={[
          [620, 116],
          [660, 116],
        ]}
      />
      <Node x={250} y={170} w={160} title="Bucket query" sub="180 buckets" />
      <Node x={470} y={170} w={150} title="metrics API" sub="?range=1h" />
      <Node x={660} y={170} w={140} title="Chart" sub="dashboard" tone="grape" />
      <Edge
        points={[
          [730, 125],
          [730, 150],
          [330, 150],
          [330, 170],
        ]}
        dashed
      />
      <Edge
        points={[
          [410, 196],
          [470, 196],
        ]}
      />
      <Edge
        points={[
          [620, 196],
          [660, 196],
        ]}
        flow
        tone="accent"
      />
      <Note x={0} y={236}>
        A container's first reading only seeds the previous-sample cache; no row is written for it.
      </Note>
    </Diagram>
  );
}

function BucketExplorer() {
  const [range, setRange] = useState<BucketRange>("1h");
  const [hasSamples, setHasSamples] = useState(true);
  const [now] = useState(() => Math.floor(Date.now() / 1000));
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
