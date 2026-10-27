package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// ingestStore is an in-memory store for the gateway, shipper, and mirror.
type ingestStore struct {
	mu       sync.Mutex
	projects []store.Project
	services []store.Service
	domains  []store.Domain
	caps     map[string]store.TelemetryCaps
	cursors  map[string]time.Time
	settings map[string]string
	events   []store.AuditEvent
}

func newIngestStore() *ingestStore {
	return &ingestStore{
		projects: []store.Project{{ID: "p1", Name: "shop"}, {ID: "p2", Name: "blog"}},
		services: []store.Service{
			{ID: "s1", ProjectID: "p1", Name: "web", Kind: "app"},
			{ID: "s2", ProjectID: "p2", Name: "db", Kind: "postgres"},
		},
		domains:  []store.Domain{{ServiceID: "s1", Host: "shop.example.com"}},
		caps:     map[string]store.TelemetryCaps{},
		cursors:  map[string]time.Time{},
		settings: map[string]string{},
	}
}

func (s *ingestStore) Projects(context.Context) ([]store.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.projects), nil
}

func (s *ingestStore) AllServices(context.Context) ([]store.Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.services), nil
}

func (s *ingestStore) AllDomains(context.Context) ([]store.Domain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.domains), nil
}

func (s *ingestStore) AllTelemetryCaps(context.Context) (map[string]store.TelemetryCaps, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps, nil
}

func (s *ingestStore) LogCursors(context.Context) (map[string]time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]time.Time, len(s.cursors))
	for k, v := range s.cursors {
		out[k] = v
	}
	return out, nil
}

func (s *ingestStore) SetLogCursor(_ context.Context, id string, ts time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[id] = ts
	return nil
}

func (s *ingestStore) DeleteLogCursor(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cursors, id)
	return nil
}

func (s *ingestStore) cursor(id string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursors[id]
}

func (s *ingestStore) AuditEventsAfter(_ context.Context, after int64, limit int) ([]store.AuditEvent, error) {
	var out []store.AuditEvent
	for _, e := range s.events {
		if e.ID > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *ingestStore) Setting(_ context.Context, key string) (string, error) {
	v, ok := s.settings[key]
	if !ok {
		return "", store.ErrNotFound
	}
	return v, nil
}

func (s *ingestStore) SetSetting(_ context.Context, key, value string) error {
	s.settings[key] = value
	return nil
}

// ingestDocker holds service containers and plays each container's output
// once.
type ingestDocker struct {
	mu         sync.Mutex
	containers []docker.Container
	output     map[string]string // by container ID: "<ts> <line>\n" lines on stdout
	since      map[string]time.Time
}

func (d *ingestDocker) List(context.Context, map[string]string) ([]docker.Container, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.containers), nil
}

func (d *ingestDocker) Inspect(_ context.Context, id string) (docker.Container, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, c := range d.containers {
		if c.ID == id {
			return c, nil
		}
	}
	return docker.Container{}, fmt.Errorf("no container %s", id)
}

// update calls fn with the container id, under the lock.
func (d *ingestDocker) update(id string, fn func(c *docker.Container)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.containers {
		if d.containers[i].ID == id {
			fn(&d.containers[i])
		}
	}
}

func (d *ingestDocker) add(c docker.Container) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers = append(d.containers, c)
}

func (d *ingestDocker) LogStream(ctx context.Context, id string, since time.Time, stdout, _ io.Writer) error {
	d.mu.Lock()
	d.since[id] = since
	out := d.output[id]
	d.mu.Unlock()
	for _, line := range strings.SplitAfter(out, "\n") {
		ts, _, _ := strings.Cut(line, " ")
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && !t.Before(since) {
			io.WriteString(stdout, line)
		}
	}
	<-ctx.Done()
	return nil
}

func (d *ingestDocker) sinceOf(id string) (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, ok := d.since[id]
	return t, ok
}

// container is a running container of a service whose init process is pid.
func container(id, serviceID, deploymentID string, pid int) docker.Container {
	return docker.Container{
		ID: id, Running: true, Pid: pid,
		Labels: map[string]string{labelService: serviceID, labelDeployment: deploymentID},
	}
}

// fakeNamespaces stands for the network namespaces of processes. A listener
// "inside" one is a real listener on the host's loopback, recorded as
// "<namespace> <address asked for>".
type fakeNamespaces struct {
	mu       sync.Mutex
	byPid    map[int]uint64
	taken    map[uint64]bool // the app listens on the port itself
	noIPv6   map[uint64]bool
	open     map[string]*nsListener
	attempts map[uint64]int // calls of Listen by namespace
}

func newFakeNamespaces() *fakeNamespaces {
	return &fakeNamespaces{
		byPid: map[int]uint64{}, taken: map[uint64]bool{}, noIPv6: map[uint64]bool{},
		open: map[string]*nsListener{}, attempts: map[uint64]int{},
	}
}

type nsListener struct {
	net.Listener
	closed atomic.Bool
}

func (l *nsListener) Close() error {
	l.closed.Store(true)
	return l.Listener.Close()
}

func (n *fakeNamespaces) set(fn func(n *fakeNamespaces)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	fn(n)
}

func (n *fakeNamespaces) ID(pid int) (uint64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ns, ok := n.byPid[pid]
	if !ok {
		return 0, fmt.Errorf("stat /proc/%d/ns/net: %w", pid, syscall.ENOENT)
	}
	return ns, nil
}

func (n *fakeNamespaces) Listen(_ context.Context, pid int, addr string) (net.Listener, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ns := n.byPid[pid]
	n.attempts[ns]++
	if host, _, _ := net.SplitHostPort(addr); host == "::1" && n.noIPv6[ns] {
		return nil, fmt.Errorf("listen tcp %s: %w", addr, syscall.EADDRNOTAVAIL)
	}
	if n.taken[ns] {
		return nil, fmt.Errorf("listen tcp %s: %w", addr, syscall.EADDRINUSE)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	l := &nsListener{Listener: ln}
	n.open[fmt.Sprintf("%d %s", ns, addr)] = l
	return l, nil
}

// listening returns the listeners still open, sorted.
func (n *fakeNamespaces) listening() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for k, l := range n.open {
		if !l.closed.Load() {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (n *fakeNamespaces) url(key string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return "http://" + n.open[key].Addr().String()
}

func (n *fakeNamespaces) attemptsIn(ns uint64) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.attempts[ns]
}

// prefixMasks masks a marker per project, to tell which mask was used.
type prefixMasks struct{}

func (prefixMasks) ShedMask(context.Context) (func(string) string, error) {
	return strings.NewReplacer("secret-p1", "***", "secret-p2", "***").Replace, nil
}

func (prefixMasks) TelemetryMask(_ context.Context, projectID, _ string) (func(string) string, error) {
	return strings.NewReplacer("secret-"+projectID, "***").Replace, nil
}

func (prefixMasks) RuntimeMask(context.Context, string) (func(string) string, error) {
	return strings.NewReplacer("hunter2", "***").Replace, nil
}

type traceSink struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (s *traceSink) ForwardOTLP(_ context.Context, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies = append(s.bodies, b)
	return nil
}

func (s *traceSink) last(t *testing.T) *coltracepb.ExportTraceServiceRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bodies) == 0 {
		t.Fatal("nothing forwarded")
	}
	req := &coltracepb.ExportTraceServiceRequest{}
	if err := proto.Unmarshal(s.bodies[len(s.bodies)-1], req); err != nil {
		t.Fatal(err)
	}
	return req
}

type discardLogs struct{}

func (discardLogs) ForwardOTLP(context.Context, []byte, []string) error { return nil }

// lockedBuffer collects log output written from several goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// gatewayFixture runs a gateway over container c1 of the app s1 in p1,
// whose init process 101 runs in namespace 1.
type gatewayFixture struct {
	g      *Gateway
	st     *ingestStore
	dc     *ingestDocker
	ns     *fakeNamespaces
	traces *traceSink
	logs   *lockedBuffer
	// loopback is the URL of the listener on the host's loopback.
	loopback string
	// stop stops Run and waits for it to return.
	stop func()
}

func newGatewayFixture(t *testing.T) *gatewayFixture {
	t.Helper()
	f := &gatewayFixture{
		st: newIngestStore(),
		dc: &ingestDocker{
			containers: []docker.Container{container("c1", "s1", "d1", 101)},
			since:      map[string]time.Time{},
		},
		ns:     newFakeNamespaces(),
		traces: &traceSink{},
		logs:   &lockedBuffer{},
	}
	f.ns.byPid[101] = 1
	f.g = NewGateway(GatewayConfig{
		Store: f.st, Docker: f.dc, Namespaces: f.ns, Masks: prefixMasks{},
		Settings: func(context.Context) (Settings, error) { return DefaultSettings(), nil },
		Port:     4318,
		Log:      slog.New(slog.NewTextHandler(f.logs, nil)),
	})
	loopback := make(chan string, 1)
	f.g.listen = func(addr string) (net.Listener, error) {
		if addr != "127.0.0.1:4318" {
			t.Errorf("host listener on %s, want 127.0.0.1:4318", addr)
		}
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			loopback <- "http://" + ln.Addr().String()
		}
		return ln, err
	}
	h, err := otlp.New(otlp.Config{
		Attribute: f.g.Attribute, Mask: f.g.Mask, Caps: f.g.Caps,
		Forwarder: NewForwarder(f.traces, f.traces, discardLogs{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.g.Run(ctx, h)
	}()
	f.stop = sync.OnceFunc(func() {
		cancel()
		<-done
	})
	t.Cleanup(f.stop)
	f.loopback = <-loopback
	f.await(t, "1 127.0.0.1:4318", "1 [::1]:4318")
	return f
}

// await kicks the gateway until exactly the listeners want are open.
func (f *gatewayFixture) await(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !slices.Equal(f.ns.listening(), want) && time.Now().Before(deadline) {
		f.g.Kick()
		time.Sleep(time.Millisecond)
	}
	if got := f.ns.listening(); !slices.Equal(got, want) {
		t.Fatalf("listening on %v, want %v", got, want)
	}
}

// post sends an OTLP/JSON trace to the listener at url and returns the
// response status.
func (f *gatewayFixture) post(t *testing.T, url, body string) int {
	t.Helper()
	resp, err := http.Post(url+"/v1/traces", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Logf("%s: %s %s", url, resp.Status, b)
	}
	return resp.StatusCode
}

// traceJSON is a trace of one server span, from a resource with
// resourceAttrs and with spanAttrs, as "key": "value" pairs.
func traceJSON(resourceAttrs, spanAttrs map[string]string) string {
	attrs := func(m map[string]string) string {
		var kv []string
		for k, v := range m {
			kv = append(kv, fmt.Sprintf(`{"key":%q,"value":{"stringValue":%q}}`, k, v))
		}
		return "[" + strings.Join(kv, ",") + "]"
	}
	return fmt.Sprintf(`{"resourceSpans":[{"resource":{"attributes":%s},"scopeSpans":[{"spans":[{
		"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"GET /",
		"kind":2,"startTimeUnixNano":"1700000000000000000","endTimeUnixNano":"1700000000100000000",
		"attributes":%s}]}]}]}`, attrs(resourceAttrs), attrs(spanAttrs))
}

// resourceAttr returns attribute key of the first forwarded resource.
func resourceAttr(req *coltracepb.ExportTraceServiceRequest, key string) string {
	for _, kv := range req.GetResourceSpans()[0].GetResource().GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func spanAttr(req *coltracepb.ExportTraceServiceRequest, key string) string {
	for _, kv := range req.GetResourceSpans()[0].GetScopeSpans()[0].GetSpans()[0].GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

func TestGatewayAttribution(t *testing.T) {
	f := newGatewayFixture(t)
	f.st.mu.Lock()
	f.st.services = append(f.st.services, store.Service{ID: "s3", ProjectID: "p2", Name: "api", Kind: "app"})
	f.st.mu.Unlock()
	f.ns.set(func(n *fakeNamespaces) { n.byPid[103] = 3 })
	f.dc.add(container("c3", "s3", "d3", 103))
	f.await(t, "1 127.0.0.1:4318", "1 [::1]:4318", "3 127.0.0.1:4318", "3 [::1]:4318")
	body := traceJSON(map[string]string{"shed.service.id": "spoofed"}, map[string]string{"token": "secret-p1 secret-p2"})

	for _, tt := range []struct {
		listener                     string
		service, project, deployment string
		name, token                  string
	}{
		{"1 127.0.0.1:4318", "s1", "shop", "d1", "web", "*** secret-p2"},
		{"3 [::1]:4318", "s3", "blog", "d3", "api", "secret-p1 ***"},
	} {
		if code := f.post(t, f.ns.url(tt.listener), body); code != http.StatusOK {
			t.Fatalf("%s: %d", tt.listener, code)
		}
		req := f.traces.last(t)
		for k, want := range map[string]string{
			"shed.service.id": tt.service, "shed.project.name": tt.project, "shed.deployment.id": tt.deployment,
			"shed.source": "otlp", "service.name": tt.name,
		} {
			if got := resourceAttr(req, k); got != want {
				t.Errorf("%s: %s = %q, want %q", tt.listener, k, got, want)
			}
		}
		if got := spanAttr(req, "token"); got != tt.token {
			t.Errorf("%s: token = %q, want %q: only the sender's project's values masked", tt.listener, got, tt.token)
		}
	}
}

func TestGatewayLoopbackAndEdgeSpans(t *testing.T) {
	tests := []struct {
		name      string
		resource  map[string]string
		span      map[string]string
		service   string
		source    string
		projectID string
	}{
		{"shed's own span", map[string]string{"shed.service.id": "shed", "service.name": "shed"},
			map[string]string{"server.address": "shop.example.com"}, "shed", "shed", ""},
		{"edge span by server.address", map[string]string{"webengine.name": "Caddy"},
			map[string]string{"server.address": "Shop.Example.com"}, "s1", "edge", "p1"},
		{"edge span by http.host with a port", map[string]string{"webengine.name": "Caddy"},
			map[string]string{"http.host": "shop.example.com:443"}, "s1", "edge", "p1"},
		{"edge span by url.full", map[string]string{"webengine.name": "Caddy"},
			map[string]string{"url.full": "https://shop.example.com/cart?x=1"}, "s1", "edge", "p1"},
		{"edge span of an unrouted host", map[string]string{"webengine.name": "Caddy"},
			map[string]string{"server.address": "shed.example.com"}, "shed", "edge", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newGatewayFixture(t)
			if code := f.post(t, f.loopback, traceJSON(tt.resource, tt.span)); code != http.StatusOK {
				t.Fatal(code)
			}
			req := f.traces.last(t)
			if got := resourceAttr(req, "shed.service.id"); got != tt.service {
				t.Errorf("shed.service.id = %q, want %q", got, tt.service)
			}
			if got := resourceAttr(req, "shed.source"); got != tt.source {
				t.Errorf("shed.source = %q, want %q", got, tt.source)
			}
			if got := resourceAttr(req, "shed.project.id"); got != tt.projectID {
				t.Errorf("shed.project.id = %q, want %q", got, tt.projectID)
			}
		})
	}
}

func TestGatewayCaps(t *testing.T) {
	f := newGatewayFixture(t)
	f.st.caps["s1"] = store.TelemetryCaps{SpansPerSecond: 1, LogsPerSecond: 2, Series: 3}
	if err := f.g.loadCaps(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := f.g.Caps("s1"), (otlp.Caps{SpansPerSecond: 1, LogRecordsPerSecond: 2, Series: 3}); got != want {
		t.Errorf("override: %+v, want %+v", got, want)
	}
	d := DefaultSettings().DefaultCaps
	if got, want := f.g.Caps("s2"), (otlp.Caps{SpansPerSecond: d.SpansPerSecond, LogRecordsPerSecond: d.LogsPerSecond, Series: d.Series}); got != want {
		t.Errorf("default: %+v, want %+v", got, want)
	}
}

func TestGatewayListenersFollowContainers(t *testing.T) {
	f := newGatewayFixture(t)
	f.st.mu.Lock()
	f.st.services = append(f.st.services, store.Service{ID: "s3", ProjectID: "p2", Name: "api", Kind: "app"})
	f.st.mu.Unlock()
	f.ns.set(func(n *fakeNamespaces) {
		n.byPid[102], n.byPid[103], n.byPid[104] = 2, 3, 4
		n.noIPv6[3] = true
	})
	stopped := container("c4", "s1", "d0", 104)
	stopped.Running = false
	f.dc.add(container("c2", "s2", "d2", 102)) // a database
	f.dc.add(stopped)
	f.dc.add(container("c3", "s3", "d3", 103)) // without IPv6
	f.await(t, "1 127.0.0.1:4318", "1 [::1]:4318", "3 127.0.0.1:4318")

	// A restarted container runs in a new namespace; its listener is
	// opened there and serves.
	f.ns.set(func(n *fakeNamespaces) {
		delete(n.byPid, 101)
		n.byPid[201] = 11
	})
	f.dc.update("c1", func(c *docker.Container) { c.Pid = 201 })
	f.await(t, "11 127.0.0.1:4318", "11 [::1]:4318", "3 127.0.0.1:4318")
	if code := f.post(t, f.ns.url("11 127.0.0.1:4318"), traceJSON(nil, nil)); code != http.StatusOK {
		t.Errorf("after a restart: %d", code)
	} else if got := resourceAttr(f.traces.last(t), "shed.service.id"); got != "s1" {
		t.Errorf("after a restart attributed to %q, want s1", got)
	}

	// A stopped container's listener closes.
	f.dc.update("c3", func(c *docker.Container) { c.Running = false })
	f.await(t, "11 127.0.0.1:4318", "11 [::1]:4318")

	// An app that listens on the port itself gets no listener: the error
	// is reported and logged once, and listening is tried again until the
	// port is free.
	f.ns.set(func(n *fakeNamespaces) {
		n.byPid[105] = 5
		n.taken[5] = true
	})
	f.dc.add(container("c5", "s3", "d5", 105))
	deadline := time.Now().Add(5 * time.Second)
	for f.ns.attemptsIn(5) < 3 && time.Now().Before(deadline) {
		f.g.Kick()
		time.Sleep(time.Millisecond)
	}
	if got := f.ns.listening(); !slices.Equal(got, []string{"11 127.0.0.1:4318", "11 [::1]:4318"}) {
		t.Errorf("listening on %v with the port taken", got)
	}
	if got := f.g.ListenerError("s3"); !strings.Contains(got, "listens on port 4318") {
		t.Errorf("ListenerError(s3) = %q, want the port named", got)
	}
	if got := f.g.ListenerError("s1"); got != "" {
		t.Errorf("ListenerError(s1) = %q, want none", got)
	}
	if n := strings.Count(f.logs.String(), "listen in container"); n != 1 {
		t.Errorf("logged the conflict %d times over %d attempts, want once", n, f.ns.attemptsIn(5))
	}
	f.ns.set(func(n *fakeNamespaces) { n.taken[5] = false })
	f.await(t, "11 127.0.0.1:4318", "11 [::1]:4318", "5 127.0.0.1:4318", "5 [::1]:4318")
	if got := f.g.ListenerError("s3"); got != "" {
		t.Errorf("ListenerError(s3) = %q once the port is free", got)
	}

	// Turning telemetry off stops Run, which closes every listener.
	f.ns.set(func(n *fakeNamespaces) { n.taken[5] = true })
	f.dc.add(container("c6", "s3", "d6", 105))
	for f.g.ListenerError("s3") == "" && time.Now().Before(deadline.Add(5*time.Second)) {
		f.g.Kick()
		time.Sleep(time.Millisecond)
	}
	f.stop()
	if got := f.ns.listening(); len(got) != 0 {
		t.Errorf("listening on %v after Run returned", got)
	}
	if got := f.g.ListenerError("s3"); got != "" {
		t.Errorf("ListenerError(s3) = %q after Run returned", got)
	}
	if code, err := http.Post(f.loopback+"/v1/traces", "application/json", strings.NewReader(traceJSON(nil, nil))); err == nil {
		code.Body.Close()
		t.Errorf("host loopback still answers after Run returned: %s", code.Status)
	}
}

func TestParseLine(t *testing.T) {
	mask := strings.NewReplacer("hunter2", "***").Replace
	long := strings.Repeat("é", maxLineBytes) // two bytes each
	manyFields := "{" + func() string {
		var kv []string
		for i := range 70 {
			kv = append(kv, fmt.Sprintf(`"f%02d":%d`, i, i))
		}
		return strings.Join(kv, ",")
	}() + "}"
	tests := []struct {
		name       string
		text       string
		wantMsg    string
		wantLevel  string
		wantFields map[string]string
		wantN      int // number of fields, if set
	}{
		{"plain", "listening on :8080", "listening on :8080", "", nil, 0},
		{"level token", "ERROR db unreachable", "ERROR db unreachable", "error", nil, 0},
		{"bracketed token", "[warn] slow request", "[warn] slow request", "warn", nil, 0},
		{"token with a colon", "INFO: started", "INFO: started", "info", nil, 0},
		{"not a token", "Errors: 0", "Errors: 0", "", nil, 0},
		{"masked text", "password=hunter2", "password=***", "", nil, 0},
		{
			"json", `{"msg":"paid","level":"INFO","trace_id":"abc","amount":12.5,"user":{"id":1},"note":"hunter2"}`,
			"paid", "info", map[string]string{"trace_id": "abc", "amount": "12.5", "user": `{"id":1}`, "note": "***"}, 4,
		},
		{"json message and severity", `{"message":"hunter2 rotated","severity":"warning"}`, "*** rotated", "warning", map[string]string{}, 0},
		{"json lvl without a message", `{"lvl":"debug","k":"v"}`, `{"lvl":"debug","k":"v"}`, "debug", map[string]string{"k": "v"}, 0},
		{"json fields capped", manyFields, manyFields, "", map[string]string{"f00": "0", "f63": "63"}, maxLineFields},
		{"invalid json", `{"msg": "half`, `{"msg": "half`, "", nil, 0},
		{"cut at 16 KiB on a rune boundary", long, long[:maxLineBytes], "", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parseLine(tt.text, mask)
			if p.msg != tt.wantMsg {
				t.Errorf("msg = %.80q, want %.80q", p.msg, tt.wantMsg)
			}
			if p.level != tt.wantLevel {
				t.Errorf("level = %q, want %q", p.level, tt.wantLevel)
			}
			for k, want := range tt.wantFields {
				if got := p.fields[k]; got != want {
					t.Errorf("field %s = %q, want %q", k, got, want)
				}
			}
			if tt.wantN > 0 && len(p.fields) != tt.wantN {
				t.Errorf("%d fields, want %d", len(p.fields), tt.wantN)
			}
		})
	}
}

// recordingLogs records inserted records and fails while fail is set.
type recordingLogs struct {
	mu      sync.Mutex
	fail    bool
	records []victoria.Record
	opts    []victoria.InsertOptions
}

func (l *recordingLogs) InsertJSONLines(_ context.Context, records []victoria.Record, opts victoria.InsertOptions) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return errors.New("store unavailable")
	}
	l.records = append(l.records, records...)
	l.opts = append(l.opts, opts)
	return nil
}

func (l *recordingLogs) messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, r := range l.records {
		out = append(out, r["_msg"])
	}
	return out
}

// countingCap admits up to limit records per service.
type countingCap struct {
	mu       sync.Mutex
	limit    int
	ingested int
}

func (c *countingCap) AdmitLogs(_ context.Context, _ string, n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := min(n, c.limit)
	c.limit -= k
	return k
}

func (c *countingCap) CountIngested(_ context.Context, _ string, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ingested += n
}

func TestLogShipperResumesFromCursor(t *testing.T) {
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ts := func(i int) time.Time { return base.Add(time.Duration(i) * time.Millisecond) }
	var out strings.Builder
	for i := range 4 {
		fmt.Fprintf(&out, "%s line %d hunter2\n", ts(i).Format(time.RFC3339Nano), i)
	}
	st := newIngestStore()
	st.cursors["c1"] = ts(1)
	st.cursors["gone"] = ts(0)
	dc := &ingestDocker{
		containers: []docker.Container{container("c1", "s1", "d1", 101)},
		output:     map[string]string{"c1": out.String()},
		since:      map[string]time.Time{},
	}
	logs := &recordingLogs{}
	lc := &countingCap{limit: 100}
	s := NewLogShipper(LogShipperConfig{Store: st, Docker: dc, Logs: logs, Masks: prefixMasks{}, Cap: lc, Log: slog.New(slog.DiscardHandler)})
	s.scanEvery, s.flushEvery = time.Hour, 10*time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	waitFor(t, func() bool { return st.cursor("c1").Equal(ts(3)) })
	cancel()
	<-done

	if since, _ := dc.sinceOf("c1"); !since.Equal(ts(1).Add(time.Nanosecond)) {
		t.Errorf("followed from %v, want just after the cursor %v", since, ts(1))
	}
	if got, want := logs.messages(), []string{"line 2 ***", "line 3 ***"}; !slices.Equal(got, want) {
		t.Errorf("shipped %q, want %q", got, want)
	}
	r := logs.records[0]
	for k, want := range map[string]string{
		"_time": ts(2).Format(time.RFC3339Nano), "stream": "stdout", "shed.source": "container",
		"shed.service.id": "s1", "shed.project.name": "shop", "shed.deployment.id": "d1",
	} {
		if r[k] != want {
			t.Errorf("record %s = %q, want %q", k, r[k], want)
		}
	}
	if !slices.Equal(logs.opts[0].StreamFields, StreamFields) {
		t.Errorf("stream fields %v, want %v", logs.opts[0].StreamFields, StreamFields)
	}
	if !st.cursor("gone").IsZero() {
		t.Error("the cursor of a removed container was kept")
	}
	if lc.ingested != 2 {
		t.Errorf("counted %d ingested, want 2", lc.ingested)
	}
}

func TestLogShipperCapAndRetry(t *testing.T) {
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	var out strings.Builder
	for i := range 3 {
		fmt.Fprintf(&out, "%s line %d\n", base.Add(time.Duration(i)).Format(time.RFC3339Nano), i)
	}
	st := newIngestStore()
	dc := &ingestDocker{
		containers: []docker.Container{container("c1", "s1", "d1", 101)},
		output:     map[string]string{"c1": out.String()},
		since:      map[string]time.Time{},
	}
	logs := &recordingLogs{fail: true}
	s := NewLogShipper(LogShipperConfig{Store: st, Docker: dc, Logs: logs, Masks: prefixMasks{}, Cap: &countingCap{limit: 2}, Log: slog.New(slog.DiscardHandler)})
	s.scanEvery, s.flushEvery, s.retryEvery = time.Hour, 10*time.Millisecond, 10*time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if !st.cursor("c1").IsZero() {
		t.Fatal("cursor advanced while the store refused the batch")
	}
	logs.mu.Lock()
	logs.fail = false
	logs.mu.Unlock()
	waitFor(t, func() bool { return st.cursor("c1").Equal(base.Add(2)) })
	cancel()
	<-done
	if got, want := logs.messages(), []string{"line 0", "line 1"}; !slices.Equal(got, want) {
		t.Errorf("shipped %q, want the first two under the cap of 2", got)
	}
}

func TestAuditMirror(t *testing.T) {
	st := newIngestStore()
	for i := range 5 {
		st.events = append(st.events, store.AuditEvent{
			ID: int64(i + 1), Time: time.Date(2026, 10, 27, 12, 0, i, 0, time.UTC),
			ActorKind: "user", Actor: "octocat", Action: "service.update", Outcome: "ok",
			ProjectID: "p1", ServiceID: map[bool]string{true: "s1"}[i%2 == 0], Detail: `{"fields":["name"]}`,
		})
	}
	st.settings[auditCursorKey] = "1"
	logs := &recordingLogs{fail: true}
	m := NewAuditMirror(st, logs, slog.New(slog.DiscardHandler))
	m.batchSize = 3
	ctx := context.Background()

	if _, err := m.mirror(ctx); err == nil {
		t.Fatal("mirror succeeded while the store refused the batch")
	}
	if st.settings[auditCursorKey] != "1" {
		t.Fatalf("cursor %s after a failed write, want 1", st.settings[auditCursorKey])
	}
	logs.fail = false
	for _, want := range []struct {
		n      int
		cursor string
	}{{3, "4"}, {1, "5"}, {0, "5"}} {
		n, err := m.mirror(ctx)
		if err != nil || n != want.n || st.settings[auditCursorKey] != want.cursor {
			t.Fatalf("mirror = %d, %v, cursor %s; want %d, cursor %s", n, err, st.settings[auditCursorKey], want.n, want.cursor)
		}
	}
	var ids []string
	for _, r := range logs.records {
		ids = append(ids, r["id"])
	}
	if want := []string{"2", "3", "4", "5"}; !slices.Equal(ids, want) {
		t.Errorf("mirrored %v, want %v", ids, want)
	}
	r := logs.records[1] // event 3, of service s1
	for k, want := range map[string]string{
		"shed.source": "audit", "shed.service.id": "s1", "shed.project.id": "p1", "action": "service.update",
		"actor": "octocat", "detail": `{"fields":["name"]}`, "_time": "2026-10-27T12:00:02Z",
	} {
		if r[k] != want {
			t.Errorf("%s = %q, want %q", k, r[k], want)
		}
	}
	if got := logs.records[0]["shed.service.id"]; got != "shed" {
		t.Errorf("event without a service attributed to %q, want shed", got)
	}
}
