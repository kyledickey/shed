package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
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

// ingestDocker holds service containers and project networks, and plays
// each container's output once.
type ingestDocker struct {
	mu         sync.Mutex
	containers []docker.Container
	networks   []docker.Network
	output     map[string]string // by container ID: "<ts> <line>\n" lines on stdout
	since      map[string]time.Time
}

func (d *ingestDocker) List(context.Context, map[string]string) ([]docker.Container, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.containers), nil
}

func (d *ingestDocker) Networks(context.Context, string) ([]docker.Network, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.networks), nil
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

func container(id, serviceID, deploymentID, network, ip string) docker.Container {
	return docker.Container{
		ID: id, Running: true,
		Labels: map[string]string{labelService: serviceID, labelDeployment: deploymentID},
		IPs:    map[string]string{network: ip},
	}
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

type gatewayFixture struct {
	g      *Gateway
	h      *otlp.Handler
	st     *ingestStore
	dc     *ingestDocker
	traces *traceSink
	now    time.Time
}

func newGatewayFixture(t *testing.T) *gatewayFixture {
	t.Helper()
	f := &gatewayFixture{
		st: newIngestStore(),
		dc: &ingestDocker{
			containers: []docker.Container{container("c1", "s1", "d1", "shed-p1", "172.18.0.2")},
			since:      map[string]time.Time{},
		},
		traces: &traceSink{},
		now:    time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}
	f.g = NewGateway(GatewayConfig{
		Store: f.st, Docker: f.dc, Masks: prefixMasks{},
		Settings: func(context.Context) (Settings, error) { return DefaultSettings(), nil },
		Port:     4318,
		Log:      slog.New(slog.DiscardHandler),
	})
	f.g.now = func() time.Time { return f.now }
	h, err := otlp.New(otlp.Config{
		Attribute: f.g.Attribute, Mask: f.g.Mask, Caps: f.g.Caps,
		Forwarder: NewForwarder(f.traces, f.traces, discardLogs{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.h = h
	if err := f.g.reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f
}

// post sends an OTLP/JSON trace to the listener info from remote.
func (f *gatewayFixture) post(info listenerInfo, remote, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote
	r = r.WithContext(context.WithValue(r.Context(), listenerKey{}, info))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
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
	p1 := listenerInfo{projectID: "p1"}
	body := traceJSON(map[string]string{"shed.service.id": "spoofed"}, map[string]string{"token": "secret-p1 secret-p2"})

	if w := f.post(p1, "172.18.0.2:40000", body); w.Code != http.StatusOK {
		t.Fatalf("known source: %d %s", w.Code, w.Body)
	}
	req := f.traces.last(t)
	for k, want := range map[string]string{
		"shed.service.id": "s1", "shed.project.name": "shop", "shed.deployment.id": "d1",
		"shed.source": "otlp", "service.name": "web",
	} {
		if got := resourceAttr(req, k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := spanAttr(req, "token"); got != "*** secret-p2" {
		t.Errorf("token = %q, want only the sender's project's values masked", got)
	}

	if w := f.post(p1, "172.18.0.9:40000", body); w.Code != http.StatusForbidden {
		t.Errorf("unknown source: %d, want 403", w.Code)
	}
	if w := f.post(listenerInfo{projectID: "p2"}, "172.18.0.2:40000", body); w.Code != http.StatusForbidden {
		t.Errorf("known address on another project's listener: %d, want 403", w.Code)
	}

	// A container that started since the last reload is found on its first
	// request, once missEvery has passed.
	f.dc.mu.Lock()
	f.dc.containers = append(f.dc.containers, container("c2", "s2", "d2", "shed-p2", "172.19.0.2"))
	f.dc.mu.Unlock()
	f.now = f.now.Add(missEvery)
	if w := f.post(listenerInfo{projectID: "p2"}, "172.19.0.2:40000", body); w.Code != http.StatusOK {
		t.Errorf("new container: %d %s, want 200", w.Code, w.Body)
	} else if got := resourceAttr(f.traces.last(t), "shed.service.id"); got != "s2" {
		t.Errorf("new container attributed to %q, want s2", got)
	}
}

func TestGatewayLoopbackAndEdgeSpans(t *testing.T) {
	loopback := listenerInfo{loopback: true}
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
			if w := f.post(loopback, "127.0.0.1:50000", traceJSON(tt.resource, tt.span)); w.Code != http.StatusOK {
				t.Fatalf("%d %s", w.Code, w.Body)
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

// fakeListener accepts nothing until closed.
type fakeListener struct {
	addr   string
	closed chan struct{}
	once   sync.Once
}

func (l *fakeListener) Accept() (net.Conn, error) { <-l.closed; return nil, net.ErrClosed }
func (l *fakeListener) Close() error              { l.once.Do(func() { close(l.closed) }); return nil }
func (l *fakeListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestGatewayListenersFollowNetworks(t *testing.T) {
	f := newGatewayFixture(t)
	var mu sync.Mutex
	open := map[string]*fakeListener{}
	failing := "172.20.0.1:4318"
	f.g.listen = func(addr string) (net.Listener, error) {
		mu.Lock()
		defer mu.Unlock()
		if addr == failing {
			return nil, errors.New("address not available")
		}
		l := &fakeListener{addr: addr, closed: make(chan struct{})}
		open[addr] = l
		return l, nil
	}
	listening := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for addr, l := range open {
			select {
			case <-l.closed:
			default:
				out = append(out, addr)
			}
		}
		slices.Sort(out)
		return out
	}
	servers := map[string]*http.Server{}
	defer func() {
		for _, s := range servers {
			s.Close()
		}
	}()
	step := func(networks []docker.Network, want []string) {
		t.Helper()
		f.dc.mu.Lock()
		f.dc.networks = networks
		f.dc.mu.Unlock()
		w, err := f.g.listeners(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		f.g.reconcile(servers, w, http.NotFoundHandler())
		deadline := time.Now().Add(5 * time.Second)
		for !slices.Equal(listening(), want) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if got := listening(); !slices.Equal(got, want) {
			t.Errorf("listening on %v, want %v", got, want)
		}
	}
	gw := func(name, addr string) docker.Network {
		return docker.Network{Name: name, Gateway: netip.MustParseAddr(addr)}
	}

	step(nil, []string{"127.0.0.1:4318"})
	step([]docker.Network{gw("shed-p1", "172.18.0.1"), gw("shed-unknown", "172.30.0.1"), {Name: "shed-p2"}},
		[]string{"127.0.0.1:4318", "172.18.0.1:4318"})
	step([]docker.Network{gw("shed-p1", "172.18.0.1"), gw("shed-p2", "172.19.0.1")},
		[]string{"127.0.0.1:4318", "172.18.0.1:4318", "172.19.0.1:4318"})
	step([]docker.Network{gw("shed-p2", "172.19.0.1")}, []string{"127.0.0.1:4318", "172.19.0.1:4318"})

	// A listener that fails to open is retried on the next pass.
	step([]docker.Network{gw("shed-p2", "172.20.0.1")}, []string{"127.0.0.1:4318"})
	failing = ""
	step([]docker.Network{gw("shed-p2", "172.20.0.1")}, []string{"127.0.0.1:4318", "172.20.0.1:4318"})
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
		containers: []docker.Container{container("c1", "s1", "d1", "shed-p1", "172.18.0.2")},
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
		containers: []docker.Container{container("c1", "s1", "d1", "shed-p1", "172.18.0.2")},
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
