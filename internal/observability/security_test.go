package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
	"github.com/kyledickey/shed/internal/victoria"
)

// session is one connection to the event stream: the events it delivers,
// then the error it ends with, or none to stay open.
type session struct {
	events []docker.Event
	err    error
}

type fakeWatcher struct {
	mu       sync.Mutex
	sessions []session
	sinces   []time.Time
	own      map[string]bool
	inspects map[string]docker.Container
	ctrs     []docker.Container
	subnets  []netip.Prefix
}

func (w *fakeWatcher) Events(ctx context.Context, since time.Time, fn func(docker.Event)) error {
	w.mu.Lock()
	w.sinces = append(w.sinces, since)
	var s session
	if len(w.sessions) > 0 {
		s, w.sessions = w.sessions[0], w.sessions[1:]
	} else {
		s.err = nil
	}
	w.mu.Unlock()
	for _, e := range s.events {
		fn(e)
	}
	if s.err != nil {
		return s.err
	}
	<-ctx.Done()
	return ctx.Err()
}

func (w *fakeWatcher) IsOwnExec(id string) bool { return w.own[id] }

func (w *fakeWatcher) Inspect(_ context.Context, id string) (docker.Container, error) {
	if c, ok := w.inspects[id]; ok {
		return c, nil
	}
	return docker.Container{}, errors.New("no such container")
}

func (w *fakeWatcher) List(context.Context, map[string]string) ([]docker.Container, error) {
	return w.ctrs, nil
}

func (w *fakeWatcher) NetworkSubnets(context.Context) ([]netip.Prefix, error) { return w.subnets, nil }

type fakeCatalog struct{}

func (fakeCatalog) Service(_ context.Context, id string) (store.Service, error) {
	if id != "svc1" {
		return store.Service{}, store.ErrNotFound
	}
	return store.Service{ID: id, ProjectID: "prj1", Name: "web", Kind: "app"}, nil
}

func (fakeCatalog) Project(_ context.Context, id string) (store.Project, error) {
	return store.Project{ID: id, Name: "shop"}, nil
}

type fakeAudit struct {
	mu     sync.Mutex
	events []audit.Event
	actors []audit.Actor
}

func (a *fakeAudit) Record(ctx context.Context, e audit.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
	a.actors = append(a.actors, audit.ActorFrom(ctx))
}

func (a *fakeAudit) all() []audit.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]audit.Event(nil), a.events...)
}

type fakeLogs struct {
	mu      sync.Mutex
	records []victoria.Record
	opts    []victoria.InsertOptions
	seed    []victoria.Record
}

func (l *fakeLogs) InsertJSONLines(_ context.Context, rs []victoria.Record, o victoria.InsertOptions) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rs...)
	l.opts = append(l.opts, o)
	return nil
}

func (l *fakeLogs) Query(context.Context, string, time.Time, time.Time, int) ([]victoria.Record, error) {
	return l.seed, nil
}

func (l *fakeLogs) all() []victoria.Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]victoria.Record(nil), l.records...)
}

type fakeConntrack struct {
	mu      sync.Mutex
	flows   []host.Flow
	err     error
	enabled int
}

func (c *fakeConntrack) Flows(context.Context) ([]host.Flow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.flows, c.err
}

func (c *fakeConntrack) EnableAccounting() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled++
	return nil
}

func (c *fakeConntrack) set(flows ...host.Flow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flows = flows
}

type secHarness struct {
	*security
	watcher *fakeWatcher
	audit   *fakeAudit
	logs    *fakeLogs
	ct      *fakeConntrack
	rec     *telemetrytest.Recorder
}

func newSecHarness(t *testing.T) *secHarness {
	t.Helper()
	rec := telemetrytest.Install(t)
	h := &secHarness{watcher: &fakeWatcher{own: map[string]bool{}, inspects: map[string]docker.Container{}}, audit: &fakeAudit{}, logs: &fakeLogs{}, ct: &fakeConntrack{}, rec: rec}
	h.security = newSecurity(Security{Docker: h.watcher, Catalog: fakeCatalog{}, Audit: h.audit, Logs: h.logs, Conntrack: h.ct},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.settle = time.Millisecond
	h.minBackoff, h.maxBackoff = time.Millisecond, 4*time.Millisecond
	return h
}

// handle delivers events to a eventFollower and waits until it has finished with them.
func (h *secHarness) handle(events ...docker.Event) {
	f := newFollower(h.security)
	for _, e := range events {
		f.handle(context.Background(), e)
	}
	f.execs.Wait()
}

var evTime = time.Unix(1_700_000_000, 0)

func containerEvent(action, id string, extra map[string]string) docker.Event {
	attrs := map[string]string{"name": "web-1", "image": "web:1", labelService: "svc1", labelProject: "prj1", labelDeployment: "dep1"}
	for k, v := range extra {
		attrs[k] = v
	}
	return docker.Event{Time: evTime, Type: "container", Action: action, ID: id, Name: "web-1", Attributes: attrs}
}

func withExit(e docker.Event, code int) docker.Event {
	e.ExitCode = code
	return e
}

func exec(id, execID, cmd string, extra map[string]string) docker.Event {
	e := containerEvent("exec_create", id, extra)
	e.ExecID, e.Command = execID, cmd
	return e
}

func TestExecAndOOMAreAudited(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(h *secHarness)
		events  []docker.Event
		want    []audit.Event
	}{
		{
			name:   "exec by a person",
			events: []docker.Event{exec("c1", "x1", "psql -c select 1", nil)},
			want: []audit.Event{{
				Action: "container.exec", Outcome: audit.OK, ProjectID: "prj1", ServiceID: "svc1", Target: "c1",
				Detail: map[string]any{"command": "psql -c select 1", "container": "web-1", "service": "web"},
			}},
		},
		{
			name:    "exec shed started itself",
			prepare: func(h *secHarness) { h.watcher.own["x1"] = true },
			events:  []docker.Event{exec("c1", "x1", "pg_dump", nil)},
		},
		{
			name: "health check run",
			prepare: func(h *secHarness) {
				h.watcher.inspects["c1"] = docker.Container{HealthCommand: "/bin/sh -c pg_isready"}
			},
			events: []docker.Event{exec("c1", "x1", "/bin/sh -c pg_isready", nil)},
		},
		{
			name: "other command in a container with a health check",
			prepare: func(h *secHarness) {
				h.watcher.inspects["c1"] = docker.Container{HealthCommand: "/bin/sh -c pg_isready"}
			},
			events: []docker.Event{exec("c1", "x1", "sh", nil)},
			want: []audit.Event{{
				Action: "container.exec", Outcome: audit.OK, ProjectID: "prj1", ServiceID: "svc1", Target: "c1",
				Detail: map[string]any{"command": "sh", "container": "web-1", "service": "web"},
			}},
		},
		{
			name: "container not started by shed",
			events: []docker.Event{{Type: "container", Action: "exec_create", ID: "c9", ExecID: "x9", Command: "sh",
				Attributes: map[string]string{"name": "other"}}},
		},
		{
			name:   "exec in a telemetry container belongs to no service",
			events: []docker.Event{exec("c2", "x2", "sh", map[string]string{labelService: "", labelTelemetry: "logs", labelProject: ""})},
			want: []audit.Event{{
				Action: "container.exec", Outcome: audit.OK, Target: "c2",
				Detail: map[string]any{"command": "sh", "container": "web-1", "service": "shed"},
			}},
		},
		{
			name:   "oom kill",
			events: []docker.Event{containerEvent("oom", "c1", nil)},
			want: []audit.Event{{
				Action: "container.oom", Outcome: audit.Failed, ProjectID: "prj1", ServiceID: "svc1", Target: "c1",
				Detail: map[string]any{"container": "web-1", "service": "web"},
			}},
		},
		{
			name:   "start is not audited",
			events: []docker.Event{containerEvent("start", "c1", nil), containerEvent("die", "c1", map[string]string{"exitCode": "1"})},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSecHarness(t)
			if tc.prepare != nil {
				tc.prepare(h)
			}
			// Telemetry is off: audit is recorded all the same.
			h.handle(tc.events...)
			got := h.audit.all()
			if len(got) != len(tc.want) {
				t.Fatalf("audit events = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i].Action != tc.want[i].Action || got[i].Outcome != tc.want[i].Outcome ||
					got[i].ProjectID != tc.want[i].ProjectID || got[i].ServiceID != tc.want[i].ServiceID ||
					got[i].Target != tc.want[i].Target || !sameDetail(got[i].Detail, tc.want[i].Detail) {
					t.Errorf("event %d = %+v, want %+v", i, got[i], tc.want[i])
				}
				if h.audit.actors[i].Kind != audit.Docker {
					t.Errorf("actor = %+v, want kind docker", h.audit.actors[i])
				}
			}
			if n := len(h.logs.all()); n != 0 {
				t.Errorf("%d records shipped while telemetry is off", n)
			}
		})
	}
}

func sameDetail(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestLongCommandIsCut(t *testing.T) {
	h := newSecHarness(t)
	cmd := make([]byte, 3*maxCommand)
	for i := range cmd {
		cmd[i] = 'é'
	}
	h.handle(exec("c1", "x1", string(cmd), nil))
	got := h.audit.all()
	if len(got) != 1 {
		t.Fatalf("audit events = %d", len(got))
	}
	if c := got[0].Detail["command"].(string); len(c) > maxCommand+len("…") {
		t.Errorf("command is %d bytes", len(c))
	}
}

func TestDockerEventsBecomeRecordsAndMetrics(t *testing.T) {
	h := newSecHarness(t)
	h.on.Store(true)
	h.handle(
		containerEvent("start", "c1", nil),
		withExit(containerEvent("die", "c1", map[string]string{"exitCode": "137"}), 137),
		containerEvent("start", "c1", nil), // a restart
		containerEvent("oom", "c1", nil),
		containerEvent("kill", "c1", map[string]string{"signal": "9"}),
		containerEvent("restart", "c1", nil),
		docker.Event{Time: evTime, Type: "image", Action: "pull", ID: "alpine:3", Name: "alpine:3"},
	)

	byMsg := map[string]victoria.Record{}
	for _, r := range h.logs.all() {
		byMsg[r["_msg"]] = r
	}
	die := byMsg["container die (exit 137)"]
	if die == nil {
		t.Fatalf("no die record in %v", byMsg)
	}
	for k, want := range map[string]string{
		"shed.source": "docker", "shed.service.id": "svc1", "shed.service.name": "web", "shed.service.kind": "app",
		"shed.project.id": "prj1", "shed.project.name": "shop", "shed.deployment.id": "dep1",
		"level": "warn", "exit_code": "137", "event": "die", "container": "web-1",
	} {
		if die[k] != want {
			t.Errorf("die[%s] = %q, want %q", k, die[k], want)
		}
	}
	if byMsg["container oom"]["level"] != "error" || byMsg["container kill (signal 9)"] == nil || byMsg["container restart"] == nil {
		t.Errorf("records = %v", byMsg)
	}
	if pull := byMsg["image pull alpine:3"]; pull == nil || pull["shed.service.id"] != "shed" || pull["shed.source"] != "docker" {
		t.Errorf("pull record = %v", pull)
	}
	if len(h.logs.all()) != 7 {
		t.Errorf("records = %d, want 7", len(h.logs.all()))
	}
	for _, o := range h.logs.opts {
		if len(o.StreamFields) != 3 {
			t.Errorf("stream fields = %v", o.StreamFields)
		}
	}

	svc := []attribute.KeyValue{
		attribute.String("shed_service_id", "svc1"), attribute.String("shed_service_name", "web"),
		attribute.String("shed_service_kind", "app"), attribute.String("shed_project_id", "prj1"),
		attribute.String("shed_project_name", "shop"),
	}
	if n := h.rec.Sum(t, "shed_container_restarts_total", svc...); n != 1 {
		t.Errorf("restarts = %d, want 1 (the second start only)", n)
	}
	if n := h.rec.Sum(t, "shed_container_oom_kills_total", svc...); n != 1 {
		t.Errorf("oom kills = %d, want 1", n)
	}
}

func TestFollowEventsReconnects(t *testing.T) {
	h := newSecHarness(t)
	t1, t2 := evTime, evTime.Add(time.Second)
	first, second := containerEvent("oom", "c1", nil), containerEvent("oom", "c2", nil)
	first.Time, second.Time = t1, t2
	h.watcher.sessions = []session{
		{events: []docker.Event{first}, err: errors.New("connection reset")},
		{err: errors.New("daemon restarting")},
		{events: []docker.Event{second}},
	}
	start := time.Now()
	h.now = func() time.Time { return start }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.followEvents(ctx); close(done) }()
	deadline := time.After(5 * time.Second)
	for len(h.audit.all()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("audit events = %d, want 2", len(h.audit.all()))
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done

	h.watcher.mu.Lock()
	defer h.watcher.mu.Unlock()
	want := []time.Time{start, t1.Add(time.Nanosecond), t1.Add(time.Nanosecond)}
	if len(h.watcher.sinces) != 3 {
		t.Fatalf("connected %d times, want 3", len(h.watcher.sinces))
	}
	for i, w := range want {
		if !h.watcher.sinces[i].Equal(w) {
			t.Errorf("connection %d since %v, want %v", i, h.watcher.sinces[i], w)
		}
	}
}

func TestFollowEventsStopsWhileWaiting(t *testing.T) {
	h := newSecHarness(t)
	h.minBackoff, h.maxBackoff = time.Hour, time.Hour
	h.watcher.sessions = []session{{err: errors.New("down")}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.followEvents(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("followEvents did not stop")
	}
}

func ep(addr string, port uint16) host.Endpoint {
	return host.Endpoint{Addr: netip.MustParseAddr(addr), Port: port}
}

func flow(src, dst string, sport, dport uint16, out, in host.Counters) host.Flow {
	return host.Flow{
		Proto:        6,
		Orig:         host.Tuple{Src: ep(src, sport), Dst: ep(dst, dport)},
		Reply:        host.Tuple{Src: ep(dst, dport), Dst: ep("203.0.113.1", sport)},
		OrigCounters: out, ReplyCounters: in,
	}
}

func ownerOf(m map[string]string) func(netip.Addr) (string, bool) {
	return func(a netip.Addr) (string, bool) {
		id, ok := m[a.String()]
		return id, ok
	}
}

func internalTo(prefixes ...string) func(netip.Addr) bool {
	return func(a netip.Addr) bool {
		for _, p := range prefixes {
			if netip.MustParsePrefix(p).Contains(a) {
				return true
			}
		}
		return false
	}
}

func TestTrackerFiltersFlows(t *testing.T) {
	owner := ownerOf(map[string]string{"172.18.0.2": "svc1"})
	internal := internalTo("172.18.0.0/16", "172.19.0.0/16")
	var tr tracker
	got := tr.update([]host.Flow{
		flow("172.18.0.2", "1.1.1.1", 4000, 443, host.Counters{}, host.Counters{}),       // egress
		flow("172.18.0.2", "172.18.0.3", 4001, 5432, host.Counters{}, host.Counters{}),   // same network
		flow("172.18.0.2", "172.19.0.9", 4002, 5432, host.Counters{}, host.Counters{}),   // other network
		flow("172.18.0.2", "127.0.0.11", 4003, 53, host.Counters{}, host.Counters{}),     // docker DNS
		flow("198.51.100.7", "172.18.0.2", 4004, 8080, host.Counters{}, host.Counters{}), // inbound
		flow("172.18.0.9", "8.8.8.8", 4005, 53, host.Counters{}, host.Counters{}),        // not a service
	}, owner, internal)
	want := dest{"svc1", netip.MustParseAddr("1.1.1.1"), 443, 6}
	if len(got) != 1 || got[want] == nil {
		t.Fatalf("destinations = %v, want only %v", got, want)
	}
}

func TestTrackerDeltas(t *testing.T) {
	owner := ownerOf(map[string]string{"172.18.0.2": "svc1", "172.18.0.4": "svc2"})
	internal := internalTo("172.18.0.0/16")
	c := func(p, b uint64) host.Counters { return host.Counters{Packets: p, Bytes: b} }
	d1 := dest{"svc1", netip.MustParseAddr("1.1.1.1"), 443, 6}
	d2 := dest{"svc2", netip.MustParseAddr("1.1.1.1"), 443, 6}

	var tr tracker
	// The first dump only establishes the baseline: the connection's total
	// is not this interval's traffic.
	got := tr.update([]host.Flow{flow("172.18.0.2", "1.1.1.1", 4000, 443, c(10, 1000), c(20, 9000))}, owner, internal)
	if got[d1] == nil || *got[d1] != (usage{}) {
		t.Fatalf("baseline usage = %+v, want present and empty", got[d1])
	}

	// Two connections of svc1 to the same destination aggregate; the old
	// one moved 5 packets, the new one counts whole. svc2 is separate.
	got = tr.update([]host.Flow{
		flow("172.18.0.2", "1.1.1.1", 4000, 443, c(15, 1500), c(25, 9500)),
		flow("172.18.0.2", "1.1.1.1", 4010, 443, c(2, 200), c(1, 60)),
		flow("172.18.0.4", "1.1.1.1", 5000, 443, c(1, 100), c(0, 0)),
	}, owner, internal)
	if u := got[d1]; u == nil || *u != (usage{flows: 2, out: c(7, 700), in: c(6, 560)}) {
		t.Errorf("svc1 usage = %+v", u)
	}
	if u := got[d2]; u == nil || *u != (usage{flows: 1, out: c(1, 100)}) {
		t.Errorf("svc2 usage = %+v", u)
	}

	// An idle connection adds nothing but stays present; a reset counter
	// is a new connection with the same addresses; a closed one is gone.
	got = tr.update([]host.Flow{
		flow("172.18.0.2", "1.1.1.1", 4000, 443, c(15, 1500), c(25, 9500)),
		flow("172.18.0.2", "1.1.1.1", 4010, 443, c(1, 40), c(1, 40)),
	}, owner, internal)
	if u := got[d1]; u == nil || *u != (usage{flows: 1, out: c(1, 40), in: c(1, 40)}) {
		t.Errorf("after reset usage = %+v", u)
	}
	if got[d2] != nil {
		t.Errorf("closed connection still present: %+v", got[d2])
	}
	if len(tr.prev) != 2 {
		t.Errorf("tracked %d connections, want 2", len(tr.prev))
	}
}

func TestSeenSet(t *testing.T) {
	var s seenSet
	key := seenKey{"svc1", netip.MustParseAddr("1.1.1.1"), 443}
	now := time.Unix(1_700_000_000, 0)
	if !s.observe(key, now) {
		t.Error("first sight is not new")
	}
	if s.observe(key, now.Add(time.Hour)) {
		t.Error("second sight is new")
	}
	if !s.observe(seenKey{"svc2", key.addr, 443}, now) {
		t.Error("another service's sight is not new")
	}
	if !s.observe(seenKey{"svc1", key.addr, 80}, now) {
		t.Error("another port is not new")
	}
	// Reached again within the window each time, a destination stays known.
	if s.observe(key, now.Add(seenWindow)) {
		t.Error("destination reached 7d after its last sight is new")
	}
	if !s.observe(key, now.Add(3*seenWindow)) {
		t.Error("destination unreached for 2 windows is not new")
	}
}

func TestSeenSetIsBounded(t *testing.T) {
	var s seenSet
	now := time.Unix(1_700_000_000, 0)
	for i := range maxSeen + 5 {
		now = now.Add(time.Second)
		s.observe(seenKey{"svc", netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 1}, now)
	}
	if len(s.at) != maxSeen {
		t.Errorf("len = %d, want %d", len(s.at), maxSeen)
	}
}

func egressHarness(t *testing.T) *secHarness {
	h := newSecHarness(t)
	h.watcher.ctrs = []docker.Container{{
		ID: "c1", IPs: map[string]string{"shed-prj1": "172.18.0.2"},
		Labels: map[string]string{labelService: "svc1", labelProject: "prj1", labelDeployment: "dep1"},
	}}
	h.watcher.subnets = []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}
	return h
}

func TestEgressRecordsAndFirstSeen(t *testing.T) {
	h := egressHarness(t)
	tr, seen := &tracker{}, &seenSet{}
	ctx := context.Background()
	c := func(p, b uint64) host.Counters { return host.Counters{Packets: p, Bytes: b} }

	h.ct.set(flow("172.18.0.2", "1.1.1.1", 4000, 443, c(10, 1000), c(10, 5000)))
	if err := h.egress(ctx, tr, seen); err != nil {
		t.Fatal(err)
	}
	// The connection existed before the worker: no traffic yet, but the
	// destination is new.
	recs := h.logs.all()
	if len(recs) != 1 || recs[0]["event"] != "egress.first_seen" {
		t.Fatalf("first dump records = %v", recs)
	}
	first := recs[0]
	for k, want := range map[string]string{
		"shed.source": "egress", "level": "warn", "shed.service.id": "svc1", "shed.service.name": "web",
		"shed.project.id": "prj1", "dst": "1.1.1.1", "dst_port": "443", "proto": "tcp",
		"_msg": "egress.first_seen 1.1.1.1:443",
	} {
		if first[k] != want {
			t.Errorf("first_seen[%s] = %q, want %q", k, first[k], want)
		}
	}

	h.ct.set(flow("172.18.0.2", "1.1.1.1", 4000, 443, c(14, 1400), c(18, 8000)))
	if err := h.egress(ctx, tr, seen); err != nil {
		t.Fatal(err)
	}
	recs = h.logs.all()
	if len(recs) != 2 {
		t.Fatalf("records = %v, want first_seen and one traffic record", recs)
	}
	r := recs[1]
	for k, want := range map[string]string{
		"shed.source": "egress", "level": "info", "shed.service.id": "svc1", "dst": "1.1.1.1", "dst_port": "443",
		"proto": "tcp", "packets": "4", "bytes": "400", "packets_in": "8", "bytes_in": "3000", "flows": "1",
		"_msg": "egress tcp 1.1.1.1:443",
	} {
		if r[k] != want {
			t.Errorf("record[%s] = %q, want %q", k, r[k], want)
		}
	}
	if r["event"] != "" {
		t.Errorf("traffic record has event %q", r["event"])
	}

	svc := []attribute.KeyValue{attribute.String("shed_service_id", "svc1"), attribute.String("shed_service_name", "web"), attribute.String("proto", "tcp")}
	if n := h.rec.Sum(t, "shed_egress_bytes_total", svc...); n != 400 {
		t.Errorf("shed_egress_bytes_total = %d, want 400", n)
	}

	// A new destination of the same service is reported once.
	h.ct.set(
		flow("172.18.0.2", "1.1.1.1", 4000, 443, c(14, 1400), c(18, 8000)),
		flow("172.18.0.2", "9.9.9.9", 4001, 853, c(1, 70), c(0, 0)),
	)
	for range 2 {
		if err := h.egress(ctx, tr, seen); err != nil {
			t.Fatal(err)
		}
	}
	firsts := 0
	for _, r := range h.logs.all() {
		if r["event"] == "egress.first_seen" {
			firsts++
		}
	}
	if firsts != 2 {
		t.Errorf("first_seen records = %d, want 2 (1.1.1.1 and 9.9.9.9 once each)", firsts)
	}
}

func TestEgressSeededFromLogs(t *testing.T) {
	h := egressHarness(t)
	h.logs.seed = []victoria.Record{
		{"shed.service.id": "svc1", "dst": "1.1.1.1", "dst_port": "443", "n": "12"},
		{"shed.service.id": "svc1", "dst": "not-an-address", "dst_port": "1"},
	}
	var seen seenSet
	h.seed(context.Background(), &seen)
	if len(seen.at) != 1 {
		t.Fatalf("seeded %d destinations, want 1", len(seen.at))
	}
	h.ct.set(flow("172.18.0.2", "1.1.1.1", 4000, 443, host.Counters{}, host.Counters{}))
	if err := h.egress(context.Background(), &tracker{}, &seen); err != nil {
		t.Fatal(err)
	}
	if recs := h.logs.all(); len(recs) != 0 {
		t.Errorf("known destination reported: %v", recs)
	}
}

func TestRunEgressEnablesAccountingAndSurvivesErrors(t *testing.T) {
	h := egressHarness(t)
	h.interval = time.Millisecond
	h.ct.err = host.ErrNotPermitted
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.runEgress(ctx); close(done) }()
	time.Sleep(20 * time.Millisecond)
	h.ct.mu.Lock()
	h.ct.err = nil
	h.ct.flows = []host.Flow{flow("172.18.0.2", "1.1.1.1", 4000, 443, host.Counters{}, host.Counters{})}
	h.ct.mu.Unlock()
	deadline := time.After(5 * time.Second)
	for len(h.logs.all()) == 0 {
		select {
		case <-deadline:
			t.Fatal("no record after the table became readable")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
	if h.ct.enabled != 1 {
		t.Errorf("accounting enabled %d times, want once", h.ct.enabled)
	}
}

func TestAlwaysWorkersRunWhileTelemetryIsOff(t *testing.T) {
	h := newHarness(t)
	started, stopped := make(chan struct{}), make(chan struct{})
	h.m.AddAlways(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(stopped)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.m.Run(ctx); close(done) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("always-on worker did not start with telemetry off")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	select {
	case <-stopped:
	default:
		t.Error("Run returned before the always-on worker stopped")
	}
}

func TestCrashedHook(t *testing.T) {
	tests := []struct {
		name   string
		events []docker.Event
		want   []Death
	}{
		{"exits non-zero on its own", []docker.Event{containerEvent("start", "c1", nil), withExit(containerEvent("die", "c1", nil), 2)},
			[]Death{{ServiceID: "svc1", Container: "web-1", ExitCode: 2}}},
		{"exits zero", []docker.Event{containerEvent("start", "c1", nil), containerEvent("die", "c1", nil)}, nil},
		{"stopped by a signal", []docker.Event{containerEvent("start", "c1", nil), containerEvent("kill", "c1", nil), withExit(containerEvent("die", "c1", nil), 143)}, nil},
		{"killed for memory", []docker.Event{containerEvent("start", "c1", nil), containerEvent("oom", "c1", nil), withExit(containerEvent("die", "c1", nil), 137)},
			[]Death{{ServiceID: "svc1", Container: "web-1", ExitCode: 137, OOM: true}}},
		{"crashes again after a stop", []docker.Event{containerEvent("kill", "c1", nil), withExit(containerEvent("die", "c1", nil), 143), containerEvent("start", "c1", nil), withExit(containerEvent("die", "c1", nil), 1)},
			[]Death{{ServiceID: "svc1", Container: "web-1", ExitCode: 1}}},
		{"not a service", []docker.Event{withExit(containerEvent("die", "c2", map[string]string{labelService: "", labelTelemetry: "logs"}), 1)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSecHarness(t)
			var got []Death
			h.Crashed = func(d Death) { got = append(got, d) }
			h.handle(tc.events...)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("deaths = %+v, want %+v", got, tc.want)
			}
		})
	}
}
