import { Diagram, Edge, Node, Note, Zone } from "../../diagram";

/** HttpChainDiagram shows what wraps a handler in internal/api. */
export function HttpChainDiagram() {
  return (
    <Diagram width={800} height={300} label="The middleware chain in internal/api">
      <Node x={10} y={20} w={90} title="Request" sub="net/http" />
      <Node x={130} y={20} w={130} title="logRequests" sub="debug log" />
      <Node x={290} y={20} w={150} title="securityHeaders" sub="CSP · XFO · nosniff" />
      <Node
        x={470}
        y={20}
        w={150}
        title="http.ServeMux"
        sub="method + path"
        tone="accent"
        emphasis
      />
      <Edge
        points={[
          [100, 46],
          [130, 46],
        ]}
      />
      <Edge
        points={[
          [260, 46],
          [290, 46],
        ]}
      />
      <Edge
        points={[
          [440, 46],
          [470, 46],
        ]}
      />
      <Edge
        points={[
          [545, 72],
          [545, 100],
        ]}
        flow
        tone="accent"
      />

      <Zone
        x={10}
        y={100}
        w={780}
        h={90}
        label="session routes · authed(pattern, handler)"
        tone="accent"
      />
      <Node x={24} y={130} w={130} title="auth.Require" sub="401 unauthorized" />
      <Node x={184} y={130} w={160} title="protectMutations" sub="403 untrusted origin" />
      <Node x={374} y={130} w={130} title="requireJSON" sub="415 not JSON" />
      <Node x={534} y={130} w={110} title="s.handle" sub="error → JSON" />
      <Node x={674} y={130} w={100} title="handler" sub="s.getProject" />
      <Edge
        points={[
          [154, 156],
          [184, 156],
        ]}
      />
      <Edge
        points={[
          [344, 156],
          [374, 156],
        ]}
      />
      <Edge
        points={[
          [504, 156],
          [534, 156],
        ]}
      />
      <Edge
        points={[
          [644, 156],
          [674, 156],
        ]}
      />

      <Zone x={10} y={205} w={780} h={85} label="routes that skip the session chain" />
      <Node
        x={24}
        y={232}
        w={250}
        title="public auth and setup routes"
        sub="login · callback · setup/*"
      />
      <Node x={294} y={232} w={160} title="webhook" sub="s.webhook, own checks" />
      <Node x={474} y={232} w={150} title="logout" sub="origin + JSON only" />
      <Node x={644} y={232} w={134} title="/api/ and /" sub="404 JSON · spa" />
    </Diagram>
  );
}

/** AdmissionDiagram shows the gates a deployment passes before it runs, and the hold that closes them. */
export function AdmissionDiagram() {
  return (
    <Diagram width={800} height={350} label="How a deployment is admitted and run by the deployer">
      <Zone x={5} y={20} w={790} h={105} label="Deployer.enqueue · under d.mu" tone="accent" />
      <Node x={15} y={52} w={170} title="d.stopped" sub="ErrStopped" />
      <Node x={200} y={52} w={170} title="d.admission" sub="ErrDeleting · Busy" />
      <Node x={385} y={52} w={170} title="d.checkFence" sub="ErrFenced" />
      <Node x={570} y={52} w={210} title="store.CreateDeployment" sub="status = queued" />
      <Edge
        points={[
          [185, 78],
          [200, 78],
        ]}
      />
      <Edge
        points={[
          [370, 78],
          [385, 78],
        ]}
      />
      <Edge
        points={[
          [555, 78],
          [570, 78],
        ]}
      />
      <Edge
        points={[
          [675, 104],
          [675, 150],
        ]}
        flow
        tone="accent"
      />

      <Zone x={5} y={140} w={790} h={90} label="worker for one service · d.workers[serviceID]" />
      <Node x={15} y={170} w={190} title="w.pending" sub="one slot, newest wins" />
      <Node x={225} y={170} w={230} title="w.cancel(errSuperseded)" sub="stops the running job" />
      <Node x={475} y={170} w={150} title="d.work loop" sub="takes pending" />
      <Node x={645} y={170} w={140} title="d.run" sub="job.execute" tone="accent" emphasis />
      <Edge
        points={[
          [205, 196],
          [225, 196],
        ]}
      />
      <Edge
        points={[
          [455, 196],
          [475, 196],
        ]}
      />
      <Edge
        points={[
          [625, 196],
          [645, 196],
        ]}
      />

      <Zone
        x={5}
        y={245}
        w={790}
        h={95}
        label="backup and restore side · a held service makes d.admission return ErrServiceBusy"
        tone="sky"
      />
      <Node x={15} y={275} w={190} title="d.reserve" sub="d.held[serviceID]" />
      <Node x={225} y={275} w={230} title="d.halt(id, errHeld)" sub="cancel, wait for w.done" />
      <Node x={475} y={275} w={150} title="*Held" sub="Stop · Release" />
      <Node x={645} y={275} w={140} title="restore fence" sub="store row" />
      <Edge
        points={[
          [205, 301],
          [225, 301],
        ]}
      />
      <Edge
        points={[
          [455, 301],
          [475, 301],
        ]}
      />
      <Edge
        points={[
          [625, 301],
          [645, 301],
        ]}
        dashed
      />
    </Diagram>
  );
}

/** LogWritersDiagram shows the writer chains that carry log text. */
export function LogWritersDiagram() {
  return (
    <Diagram
      width={800}
      height={312}
      label="The writer chains behind build, runtime, and shed logs"
    >
      <Note x={10} y={24}>
        build output
      </Note>
      <Node x={10} y={32} w={150} h={44} title="build.Builder" sub="git · railpack" />
      <Node
        x={190}
        y={32}
        w={140}
        h={44}
        title="build.Redactor"
        sub="clone URL + env"
        tone="accent"
        emphasis
      />
      <Node x={360} y={32} w={120} h={44} title="syncWriter" sub="one lock" />
      <Node x={510} y={32} w={130} h={44} title="cappedWriter" sub="log_max_mb" />
      <Node x={670} y={32} w={120} h={44} title="<id>.log" sub="0600" />
      <Edge
        points={[
          [160, 54],
          [190, 54],
        ]}
      />
      <Edge
        points={[
          [330, 54],
          [360, 54],
        ]}
      />
      <Edge
        points={[
          [480, 54],
          [510, 54],
        ]}
      />
      <Edge
        points={[
          [640, 54],
          [670, 54],
        ]}
      />

      <Note x={10} y={102}>
        container output while it boots
      </Note>
      <Node x={10} y={110} w={150} h={44} title="docker.Logs" sub="follow from start" />
      <Node
        x={190}
        y={110}
        w={140}
        h={44}
        title="build.Redactor"
        sub="j.secrets"
        tone="accent"
        emphasis
      />
      <Node x={360} y={110} w={140} h={44} title="deploy.lineWriter" sub="drops timestamps" />
      <Node x={530} y={110} w={110} h={44} title="j.out" sub="same chain" />
      <Edge
        points={[
          [160, 132],
          [190, 132],
        ]}
      />
      <Edge
        points={[
          [330, 132],
          [360, 132],
        ]}
      />
      <Edge
        points={[
          [500, 132],
          [530, 132],
        ]}
      />

      <Note x={10} y={180}>
        runtime logs
      </Note>
      <Node x={10} y={188} w={150} h={44} title="docker.Logs" sub="tail 500, follow" />
      <Node
        x={190}
        y={188}
        w={140}
        h={44}
        title="build.Redactor"
        sub="logSecrets"
        tone="accent"
        emphasis
      />
      <Node x={360} y={188} w={140} h={44} title="api.lineWriter" sub="64 KiB lines" />
      <Node x={530} y={188} w={150} h={44} title="startSSE" sub="event: log" />
      <Edge
        points={[
          [160, 210],
          [190, 210],
        ]}
      />
      <Edge
        points={[
          [330, 210],
          [360, 210],
        ]}
      />
      <Edge
        points={[
          [500, 210],
          [530, 210],
        ]}
      />

      <Note x={10} y={254}>
        shed's own log
      </Note>
      <Node x={10} y={262} w={150} h={44} title="slog.Logger" sub="text handler" />
      <Node
        x={190}
        y={262}
        w={190}
        h={44}
        title="io.MultiWriter"
        sub="stderr · Tail · lumberjack"
      />
      <Node x={410} y={262} w={140} h={44} title="logtail.Tail" sub="ring, 1000" />
      <Node x={580} y={262} w={150} h={44} title="Tail.Follow" sub="256-line buffer" />
      <Edge
        points={[
          [160, 284],
          [190, 284],
        ]}
      />
      <Edge
        points={[
          [380, 284],
          [410, 284],
        ]}
      />
      <Edge
        points={[
          [550, 284],
          [580, 284],
        ]}
      />
    </Diagram>
  );
}

/** CollectorDiagram shows one tick of the metrics collector. */
export function CollectorDiagram() {
  return (
    <Diagram width={800} height={110} label="One tick of the metrics collector">
      <Node x={10} y={30} w={140} title="docker.List" sub="label shed.service" />
      <Node x={180} y={30} w={130} title="docker.Stats" sub="one shot each" />
      <Node x={340} y={30} w={130} title="c.prev" sub="last reading" />
      <Node x={500} y={30} w={130} title="rates()" sub="Δ / Δt" tone="accent" emphasis />
      <Node x={660} y={30} w={130} title="InsertMetric" sub="per-service sum" />
      <Edge
        points={[
          [150, 56],
          [180, 56],
        ]}
      />
      <Edge
        points={[
          [310, 56],
          [340, 56],
        ]}
      />
      <Edge
        points={[
          [470, 56],
          [500, 56],
        ]}
      />
      <Edge
        points={[
          [630, 56],
          [660, 56],
        ]}
      />
      <Note x={10} y={20} mono>
        Collector.collect · every 10s
      </Note>
    </Diagram>
  );
}

/** BackupQueueDiagram shows how backup and restore jobs reach the single worker. */
export function BackupQueueDiagram() {
  return (
    <Diagram width={800} height={165} label="How backup jobs are queued and run one at a time">
      <Node x={10} y={6} w={150} h={40} title="BackUp" sub="manual" />
      <Node x={10} y={56} w={150} h={40} title="tick" sub="cron, each minute" />
      <Node x={10} y={106} w={150} h={40} title="Restore" sub="a backup row" />
      <Edge
        points={[
          [160, 26],
          [195, 26],
          [195, 76],
          [230, 76],
        ]}
      />
      <Edge
        points={[
          [160, 76],
          [230, 76],
        ]}
      />
      <Edge
        points={[
          [160, 126],
          [195, 126],
          [195, 76],
          [230, 76],
        ]}
      />
      <Node
        x={230}
        y={50}
        w={150}
        title="Manager.queue"
        sub="[]*job under m.mu"
        tone="accent"
        emphasis
      />
      <Node x={420} y={50} w={150} title="take()" sub="sets m.running" />
      <Node x={610} y={50} w={170} title="execute" sub="runBackup | runRestore" />
      <Edge
        points={[
          [380, 76],
          [420, 76],
        ]}
        flow
        tone="accent"
      />
      <Edge
        points={[
          [570, 76],
          [610, 76],
        ]}
      />
    </Diagram>
  );
}

/** ArchiveDiagram shows the writers between a backup's source and its file. */
export function ArchiveDiagram() {
  return (
    <Diagram width={800} height={90} label="The pipeline that writes one backup archive">
      <Node x={5} y={20} w={115} title="fill(w)" sub="dump | tar" />
      <Node x={140} y={20} w={115} title="zstd" sub="level" />
      <Node x={275} y={20} w={115} title="age" sub="X25519" />
      <Node x={410} y={20} w={125} title=".partial" sub="0600 · O_EXCL" />
      <Node x={555} y={20} w={115} title="fsync, rename" sub="syncDir" />
      <Node x={690} y={20} w={105} title="upload" sub="optional" />
      <Edge
        points={[
          [120, 46],
          [140, 46],
        ]}
      />
      <Edge
        points={[
          [255, 46],
          [275, 46],
        ]}
      />
      <Edge
        points={[
          [390, 46],
          [410, 46],
        ]}
      />
      <Edge
        points={[
          [535, 46],
          [555, 46],
        ]}
      />
      <Edge
        points={[
          [670, 46],
          [690, 46],
        ]}
        dashed
      />
    </Diagram>
  );
}

/** RestoreDiagram shows the steps of a volume restore and what the fence records. */
export function RestoreDiagram() {
  return (
    <Diagram width={800} height={150} label="The steps of a volume restore and its fence phases">
      <Node x={10} y={20} w={136} title="pre-restore" sub="inline backup" />
      <Node x={168} y={20} w={136} title="Hold" sub="services.Hold" tone="sky" />
      <Node x={326} y={20} w={136} title="retaining" sub="copy aside" tone="accent" emphasis />
      <Node x={484} y={20} w={136} title="replacing" sub="empty, extract" tone="accent" emphasis />
      <Node x={642} y={20} w={148} title="unfence" sub="verify, Release" />
      <Edge
        points={[
          [146, 46],
          [168, 46],
        ]}
      />
      <Edge
        points={[
          [304, 46],
          [326, 46],
        ]}
      />
      <Edge
        points={[
          [462, 46],
          [484, 46],
        ]}
      />
      <Edge
        points={[
          [620, 46],
          [642, 46],
        ]}
      />
      <Note x={10} y={100}>
        Failure while replacing: putBack from the -pre-restore volumes, then unfence.
      </Note>
      <Note x={10} y={120}>
        shed restarts while fenced: Recover → recoverFence, by phase.
      </Note>
    </Diagram>
  );
}
