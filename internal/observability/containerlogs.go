package observability

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// LogShipperStore names services and keeps each container's log cursor.
// *store.Store implements it.
type LogShipperStore interface {
	DirectoryStore
	LogCursors(ctx context.Context) (map[string]time.Time, error)
	SetLogCursor(ctx context.Context, containerID string, ts time.Time) error
	DeleteLogCursor(ctx context.Context, containerID string) error
}

var _ LogShipperStore = (*store.Store)(nil)

// LogShipperDocker lists service containers and follows their output.
// *docker.Client implements it.
type LogShipperDocker interface {
	ContainerLister
	LogStream(ctx context.Context, id string, since time.Time, stdout, stderr io.Writer) error
}

var _ LogShipperDocker = (*docker.Client)(nil)

// LogWriter writes records to VictoriaLogs. *victoria.Logs implements it.
type LogWriter interface {
	InsertJSONLines(ctx context.Context, records []victoria.Record, opts victoria.InsertOptions) error
}

var _ LogWriter = (*victoria.Logs)(nil)

// RuntimeMasks build the function that masks a container's output.
// *control.Plane implements it.
type RuntimeMasks interface {
	RuntimeMask(ctx context.Context, deploymentID string) (func(string) string, error)
}

// LogCap is the per-service log cap shared with the gateway's OTLP logs.
// *otlp.Handler implements it.
type LogCap interface {
	AdmitLogs(ctx context.Context, serviceID string, n int) int
	CountIngested(ctx context.Context, signal string, n int)
}

var _ LogCap = (*otlp.Handler)(nil)

// LogShipperConfig configures a LogShipper.
type LogShipperConfig struct {
	Store  LogShipperStore
	Docker LogShipperDocker
	Logs   LogWriter
	Masks  RuntimeMasks
	Cap    LogCap
	Log    *slog.Logger
}

// LogShipper ships the output of every service container to VictoriaLogs.
type LogShipper struct {
	cfg LogShipperConfig

	// Timings and sizes, replaced in tests.
	scanEvery  time.Duration // how often containers are listed
	flushEvery time.Duration // the longest a line waits in a batch
	maxBatch   int           // bytes of lines that flush a batch at once
	retryEvery time.Duration // wait after a failed write
}

// NewLogShipper returns a LogShipper. It ships only while Run runs.
func NewLogShipper(cfg LogShipperConfig) *LogShipper {
	return &LogShipper{
		cfg:        cfg,
		scanEvery:  5 * time.Second,
		flushEvery: time.Second,
		maxBatch:   1 << 20,
		retryEvery: 5 * time.Second,
	}
}

// shippedLine is a line ready to ship.
type shippedLine struct {
	container string
	serviceID string
	time      time.Time
	record    victoria.Record
	size      int
}

// follower follows one container's output.
type follower struct {
	cancel context.CancelFunc
	done   chan struct{}
	// last is the time of the last line read; read it only after done.
	last time.Time
}

// Run ships container output until ctx is done. It is meant to run as a
// [Worker].
func (s *LogShipper) Run(ctx context.Context) {
	lines := make(chan shippedLine, 1024)
	batched := make(chan struct{})
	go func() {
		defer close(batched)
		s.batch(ctx, lines)
	}()
	defer func() { <-batched }()

	followers := make(map[string]*follower)
	read := make(map[string]time.Time) // last line read, by container, in this run
	defer func() {
		for _, f := range followers {
			f.cancel()
			<-f.done
		}
	}()
	tick := time.NewTicker(s.scanEvery)
	defer tick.Stop()
	for {
		if err := s.scan(ctx, followers, read, lines); err != nil && ctx.Err() == nil {
			s.cfg.Log.Warn("ship container logs", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// scan follows every running service container not followed yet, from
// where its shipping stopped, and stops following and forgets the cursor of
// containers that are gone.
func (s *LogShipper) scan(ctx context.Context, followers map[string]*follower, read map[string]time.Time, out chan<- shippedLine) error {
	d, err := loadDirectory(ctx, s.cfg.Store, s.cfg.Docker)
	if err != nil {
		return err
	}
	cursors, err := s.cfg.Store.LogCursors(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(d.containers))
	for _, c := range d.containers {
		present[c.ID] = true
		if f := followers[c.ID]; f != nil {
			select {
			case <-f.done:
				read[c.ID] = f.last
				delete(followers, c.ID)
			default:
				continue
			}
		}
		if !c.Running {
			continue
		}
		since := cursors[c.ID]
		if r := read[c.ID]; r.After(since) {
			since = r
		}
		if !since.IsZero() {
			// Docker includes lines at since itself, which were shipped.
			since = since.Add(time.Nanosecond)
		}
		fctx, cancel := context.WithCancel(ctx)
		f := &follower{cancel: cancel, done: make(chan struct{}), last: read[c.ID]}
		followers[c.ID] = f
		go s.follow(fctx, c, since, f, out)
	}
	for id, f := range followers {
		if !present[id] {
			f.cancel()
			<-f.done
			delete(followers, id)
		}
	}
	for id := range read {
		if !present[id] {
			delete(read, id)
		}
	}
	for id := range cursors {
		if !present[id] {
			if err := s.cfg.Store.DeleteLogCursor(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// follow reads a container's output from since until it stops or ctx is
// done, sending each line to out.
func (s *LogShipper) follow(ctx context.Context, c serviceContainer, since time.Time, f *follower, out chan<- shippedLine) {
	defer close(f.done)
	defer f.cancel()
	mask, err := s.cfg.Masks.RuntimeMask(ctx, c.Source.DeploymentID)
	if err != nil {
		s.cfg.Log.Warn("ship container logs: masks", "container", c.ID, "err", err)
		return
	}
	emit := func(l logLine) {
		r := lineRecord(c.Source, l, parseLine(l.text, mask))
		select {
		case out <- shippedLine{container: c.ID, serviceID: c.Source.ServiceID, time: l.time, record: r, size: len(l.text) + 256}:
			f.last = l.time
		case <-ctx.Done():
		}
	}
	stdout := &lineWriter{stream: "stdout", emit: emit}
	stderr := &lineWriter{stream: "stderr", emit: emit}
	if err := s.cfg.Docker.LogStream(ctx, c.ID, since, stdout, stderr); err != nil {
		s.cfg.Log.Warn("ship container logs: follow", "container", c.ID, "err", err)
	}
	stdout.flush()
	stderr.flush()
}

// lineRecord returns the VictoriaLogs record of a container's line.
func lineRecord(src otlp.Source, l logLine, p parsedLine) victoria.Record {
	r := make(victoria.Record, len(p.fields)+12)
	for k, v := range p.fields {
		if !strings.HasPrefix(k, "_") {
			r[k] = v
		}
	}
	r["_time"] = l.time.UTC().Format(time.RFC3339Nano)
	r["_msg"] = p.msg
	r["stream"] = l.stream
	if p.level != "" {
		r["level"] = p.level
	}
	src.Origin = "container"
	stampRecord(r, src)
	return r
}

// stampRecord sets the label schema's fields of src on r.
func stampRecord(r victoria.Record, src otlp.Source) {
	r["shed.project.id"] = src.ProjectID
	r["shed.project.name"] = src.ProjectName
	r["shed.service.id"] = src.ServiceID
	r["shed.service.name"] = src.ServiceName
	r["shed.service.kind"] = src.ServiceKind
	r["shed.deployment.id"] = src.DeploymentID
	r["shed.source"] = src.Origin
}

// lineWriter splits Docker's timestamped output into lines. A line longer
// than maxLineBytes keeps its start; the rest is discarded.
type lineWriter struct {
	stream string
	emit   func(logLine)
	buf    []byte
	long   bool // the current line overflowed buf
}

// maxPrefix is room for Docker's timestamp and the space after it.
const maxPrefix = 64

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if room := maxLineBytes + maxPrefix - len(w.buf); !w.long && len(chunk) > room {
			w.buf = append(w.buf, chunk[:room]...)
			w.long = true
		} else if !w.long {
			w.buf = append(w.buf, chunk...)
		}
		if i < 0 {
			break
		}
		w.flush()
		p = p[i+1:]
	}
	return n, nil
}

// flush emits the buffered line, if any.
func (w *lineWriter) flush() {
	if len(w.buf) == 0 && !w.long {
		return
	}
	line := strings.TrimSuffix(string(w.buf), "\r")
	w.buf, w.long = w.buf[:0], false
	l := logLine{stream: w.stream, text: line, time: time.Now()}
	if ts, rest, ok := strings.Cut(line, " "); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			l.time, l.text = t, rest
		}
	}
	w.emit(l)
}

// batch collects lines and ships them at most flushEvery after the first
// of a batch arrived, or once they reach maxBatch bytes.
func (s *LogShipper) batch(ctx context.Context, in <-chan shippedLine) {
	var pending []shippedLine
	size := 0
	var timer <-chan time.Time
	flush := func() {
		s.ship(ctx, pending)
		pending, size, timer = nil, 0, nil
	}
	for {
		select {
		case <-ctx.Done():
			return
		case l := <-in:
			pending = append(pending, l)
			size += l.size
			if len(pending) == 1 {
				timer = time.After(s.flushEvery)
			}
			if size >= s.maxBatch {
				flush()
			}
		case <-timer:
			flush()
		}
	}
}

// ship writes a batch, less the lines over their service's log cap, retrying
// until the store accepts it or ctx is done, then advances the cursor of
// every container in it.
func (s *LogShipper) ship(ctx context.Context, batch []shippedLine) {
	counts := make(map[string]int)
	for _, l := range batch {
		counts[l.serviceID]++
	}
	admitted := make(map[string]int, len(counts))
	for id, n := range counts {
		admitted[id] = s.cfg.Cap.AdmitLogs(ctx, id, n)
	}
	records := make([]victoria.Record, 0, len(batch))
	last := make(map[string]time.Time)
	for _, l := range batch {
		if admitted[l.serviceID] > 0 {
			admitted[l.serviceID]--
			records = append(records, l.record)
		}
		if l.time.After(last[l.container]) {
			last[l.container] = l.time
		}
	}
	for {
		err := s.cfg.Logs.InsertJSONLines(ctx, records, victoria.InsertOptions{StreamFields: StreamFields})
		if err == nil {
			break
		}
		s.cfg.Log.Warn("ship container logs: write", "records", len(records), "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.retryEvery):
		}
	}
	s.cfg.Cap.CountIngested(ctx, "logs", len(records))
	for id, ts := range last {
		if err := s.cfg.Store.SetLogCursor(ctx, id, ts); err != nil {
			s.cfg.Log.Warn("ship container logs: cursor", "container", id, "err", err)
		}
	}
}
