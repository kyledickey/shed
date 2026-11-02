//go:build integration

// This test needs Docker and network access, and that no shed-telemetry-*
// containers or volumes exist. It runs the real Manager and exercises what
// reads the stores: the sampler's push and the charts built from it, log
// history, and the MCP telemetry tools. Run it under the shared lock:
//
//	flock /tmp/shed-telemetry-integration.lock go test -tags integration -run IntegrationAgents -v -timeout 25m ./internal/observability/
package observability_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/kyledickey/shed/internal/api"
	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/mcp"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/variables"
	"github.com/kyledickey/shed/internal/victoria"
)

// fakeClock hands out a synthetic time that advances ten seconds per call
// until it reaches end, then stays there.
type fakeClock struct {
	mu    sync.Mutex
	base  time.Time
	calls int
	end   int
	last  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls < c.end {
		c.calls++
	}
	c.last = c.base.Add(time.Duration(c.calls) * 10 * time.Second)
	return c.last
}

func (c *fakeClock) done() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls >= c.end
}

func (c *fakeClock) secs() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last.Sub(c.base).Seconds()
}

// world is a container and a host whose counters follow a fakeClock.
type world struct {
	c   *fakeClock
	svc string
}

func (w world) List(context.Context, map[string]string) ([]docker.Container, error) {
	return []docker.Container{{ID: "c1", Running: true, Labels: map[string]string{"shed.service": w.svc}}}, nil
}

func (w world) Inspect(context.Context, string) (docker.Container, error) {
	return docker.Container{CPULimit: 1.5, MemoryLimit: 256 << 20}, nil
}

func (w world) Stats(context.Context, string) (docker.Stats, error) {
	s := w.c.secs()
	return docker.Stats{
		Read: w.c.last, CPUTotal: uint64(s * 0.2e9), MemoryUsage: uint64(100+int(s/10)) << 20,
		NetRx: uint64(s * 1000), NetTx: uint64(s * 300), DiskRead: uint64(s * 10), DiskWrite: uint64(s * 20),
	}, nil
}

func (w world) Read(context.Context) (host.Stats, error) {
	s := w.c.secs()
	return host.Stats{
		CPUs: 2, CPUTotal: uint64(s * 200), CPUIdle: uint64(s * 150),
		MemoryTotal: 8 << 30, MemoryAvailable: 4<<30 - uint64(s)*1000, NetRx: uint64(s * 500), NetTx: uint64(s * 100),
		DiskRead: uint64(s * 50), DiskWrite: uint64(s * 60), DiskTotal: 100 << 30, DiskUsed: 20<<30 + uint64(s)*4096,
	}, nil
}

// sample runs a collector for 14 ticks of synthetic time, then leaves the
// clock at the end.
func sample(t *testing.T, base time.Time, svc string, st *store.Store, remote metrics.Remote) *metrics.Collector {
	t.Helper()
	clock := &fakeClock{base: base, end: 14}
	w := world{c: clock, svc: svc}
	c := metrics.New(metrics.Config{
		Docker: w, Host: w, Store: st, Remote: remote, Interval: 10 * time.Millisecond,
		Now: clock.Now, Log: slog.New(slog.DiscardHandler),
	})
	c.SetRemote(remote != nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	for !clock.done() {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // the last tick finishes
	cancel()
	<-done
	return c
}

func TestIntegrationAgents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		name := "shed-telemetry-" + c
		if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output(); len(out) > 0 {
			t.Fatalf("container %s exists; this test would replace it", name)
		}
		if exec.Command("docker", "volume", "inspect", name).Run() == nil {
			t.Fatalf("volume %s exists; this test would delete it", name)
		}
		t.Cleanup(func() {
			if t.Failed() {
				logs, _ := exec.Command("docker", "logs", "--tail", "30", name).CombinedOutput()
				t.Logf("%s logs:\n%s", name, logs)
			}
			exec.Command("docker", "rm", "-f", name).Run()
			exec.Command("docker", "volume", "rm", "-f", name).Run()
		})
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dc, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()

	var handler atomic.Value
	shed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.Load().(http.Handler).ServeHTTP(w, r)
	}))
	defer shed.Close()

	addrs := observability.Addrs{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t), Grafana: freeAddr(t)}
	vm := victoria.NewMetrics("http://"+addrs.Metrics, nil)
	vl := victoria.NewLogs("http://"+addrs.Logs, nil)
	vt := victoria.NewTraces("http://"+addrs.Traces, nil)
	stack := observability.New(observability.Config{
		Store: st, Docker: dc, Metrics: vm, Logs: vl, Traces: vt,
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs: addrs, Dir: filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL: shed.URL + "/grafana/", OTLPEndpoint: "http://localhost:4318", Log: log,
	})
	dashboards := observability.NewDashboards(observability.DashboardsConfig{
		Store: st,
		Grafana: func(ctx context.Context) (observability.DashboardAPI, error) {
			pw, err := stack.AdminPassword(ctx)
			if err != nil {
				return nil, err
			}
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second}), nil
		},
		GrafanaURL: shed.URL + "/grafana/", Log: log,
	})
	stack.AddWorker(dashboards.Run)

	// The service under test and a neighbor whose logs must stay out of its.
	ctxbg := context.Background()
	prj, err := st.CreateProject(ctxbg, "acme")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctxbg, store.Service{ProjectID: prj.ID, Name: "web", Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateService(ctxbg, store.Service{ProjectID: prj.ID, Name: "worker", Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	const secretValue = "sk-live-abcdef123456"
	if err := st.SetVariables(ctxbg, svc.ID, map[string]string{"API_KEY": secretValue}); err != nil {
		t.Fatal(err)
	}

	// Two samplers see the same synthetic ticks: one pushes to
	// VictoriaMetrics, the other writes SQLite.
	base := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	stRemote := st
	stLocal, err := store.Open(filepath.Join(t.TempDir(), "local.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer stLocal.Close()
	for _, s := range []*store.Store{stLocal} {
		p, _ := s.CreateProject(ctxbg, "acme")
		if _, err := s.CreateService(ctxbg, store.Service{ProjectID: p.ID, Name: "web", Kind: "app"}); err != nil {
			t.Fatal(err)
		}
	}
	localSvcs, _ := stLocal.AllServices(ctxbg)
	local := sample(t, base, localSvcs[0].ID, stLocal, nil)

	remoteClock := &fakeClock{base: base, end: 14}
	rw := world{c: remoteClock, svc: svc.ID}
	remoteCollector := metrics.New(metrics.Config{
		Docker: rw, Host: rw, Store: stRemote, Remote: vm, Interval: 10 * time.Millisecond,
		Now: remoteClock.Now, Log: log,
	})
	stack.OnEnabled(remoteCollector.SetRemote)

	auditLog := audit.New(st, log)
	plane := control.New(control.Config{
		Store: st, Audit: auditLog, Telemetry: stack, Metrics: remoteCollector,
		Variables:  variables.New(st, nil),
		Stores:     control.TelemetryStores{Metrics: vm, Logs: vl, Traces: vt},
		Dashboards: dashboards, BaseURL: shed.URL, Log: log,
	})
	authn := auth.New(api.AuthStore(st), auditLog, func() (auth.OAuth, bool) { return nil, false }, shed.URL, []string{"octocat"}, log)
	mcpHandler := mcp.New(mcp.Config{Backend: plane, Version: "it", Log: log}).Handler()
	srv, err := api.New(ctx, api.Config{
		Control: plane, Settings: st, Auth: authn, GitHub: &api.GitHubHolder{}, BaseURL: shed.URL,
		Web: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, MCP: mcpHandler, GrafanaAddr: addrs.Grafana, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.Store(srv.Handler())
	if err := st.UpsertUser(ctxbg, store.User{GitHubID: 1, Login: "octocat"}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("token"))
	if err := st.CreateSession(ctxbg, hex.EncodeToString(sum[:]), 1, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "shed_session", Value: "token"}
	get := func(path string, v any) *http.Response {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, "GET", shed.URL+path, nil)
		req.AddCookie(session)
		resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if v != nil {
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: %s", path, resp.Status)
			}
			if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
		}
		return resp
	}

	// Off: the history API answers 409, the MCP tools say so.
	if code := get("/api/services/"+svc.ID+"/logs/history", nil).StatusCode; code != http.StatusConflict {
		t.Errorf("history while off = %d, want 409", code)
	}

	on := observability.DefaultSettings()
	on.Enabled = true
	if _, err := plane.SetTelemetry(ctx, on); err != nil {
		t.Fatal(err)
	}
	if err := stack.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		off := on
		off.Enabled = false
		plane.SetTelemetry(context.Background(), off)
		stack.Reconcile(context.Background())
	})

	eventually := func(what string, f func() bool) {
		t.Helper()
		deadline := time.Now().Add(90 * time.Second)
		for !f() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(time.Second)
		}
	}

	t.Run("charts", func(t *testing.T) {
		// Run the remote sampler against the real VictoriaMetrics.
		runCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { remoteCollector.Run(runCtx); close(done) }()
		for !remoteClock.done() {
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(500 * time.Millisecond)
		stop()
		<-done

		if bs, err := stRemote.MetricBuckets(ctx, svc.ID, base, 20*time.Second, 100); err != nil || len(bs) != 0 {
			t.Errorf("SQLite holds %d buckets of pushed samples (%v), want none", len(bs), err)
		}
		want, err := local.Query(ctx, localSvcs[0].ID, metrics.Range1h)
		if err != nil {
			t.Fatal(err)
		}
		points := func(s []*float64) int {
			n := 0
			for _, p := range s {
				if p != nil {
					n++
				}
			}
			return n
		}
		if points(want.CPU) < 5 {
			t.Fatalf("the SQLite path has %d points; the synthetic ticks are wrong", points(want.CPU))
		}
		var got struct {
			Start                        time.Time
			Step                         float64
			CPULimit                     float64
			MemoryLimit                  int64
			CPU, Memory, NetRx, DiskRead []*float64
		}
		eventually("charts from VictoriaMetrics", func() bool {
			get("/api/services/"+svc.ID+"/metrics?range=1h", &got)
			return points(got.CPU) == points(want.CPU)
		})
		if !got.Start.Equal(want.Start) || got.Step != want.Step.Seconds() || got.CPULimit != 1.5 || got.MemoryLimit != 256<<20 {
			t.Errorf("window: start %v step %v limits %v %v; want %v %v", got.Start, got.Step, got.CPULimit, got.MemoryLimit, want.Start, want.Step)
		}
		near := func(name string, a, b []*float64) {
			t.Helper()
			for i := range a {
				if (a[i] == nil) != (b[i] == nil) {
					t.Errorf("%s[%d]: %v from VictoriaMetrics, %v from SQLite", name, i, a[i], b[i])
				} else if a[i] != nil && math.Abs(*a[i]-*b[i]) > 1e-6*math.Max(1, math.Abs(*b[i])) {
					t.Errorf("%s[%d] = %v from VictoriaMetrics, %v from SQLite", name, i, *a[i], *b[i])
				}
			}
		}
		near("cpu", got.CPU, want.CPU)
		near("memory", got.Memory, want.Memory)
		near("netRx", got.NetRx, want.NetRx)
		near("diskRead", got.DiskRead, want.DiskRead)

		var host struct {
			Start                      time.Time
			CPUs                       int
			MemoryTotal, DiskTotal     int64
			CPU, Memory, DiskUsed, Net []*float64
		}
		wantHost, err := local.QueryHost(ctx, metrics.Range1h)
		if err != nil {
			t.Fatal(err)
		}
		eventually("host charts from VictoriaMetrics", func() bool {
			get("/api/host/metrics?range=1h", &host)
			return points(host.CPU) == points(wantHost.CPU)
		})
		near("host cpu", host.CPU, wantHost.CPU)
		near("host memory", host.Memory, wantHost.Memory)
		near("host diskUsed", host.DiskUsed, wantHost.DiskUsed)
		if !host.Start.Equal(wantHost.Start) || host.MemoryTotal != 8<<30 {
			t.Errorf("host window %v total %d", host.Start, host.MemoryTotal)
		}
		res, err := vm.Query(ctx, `last_over_time(shed_host_disk_total_bytes[1h])`, time.Time{})
		if err != nil || len(res.Series) != 1 || res.Series[0].Values[0].Value != 100<<30 {
			t.Errorf("host disk total: %+v, %v", res, err)
		}
	})

	// Logs: 60 records of the service (a third mention "needle"), 20 of its
	// neighbor, a leak of a variable value, and edge access records.
	now := time.Now()
	var recs []victoria.Record
	rec := func(id, source, msg, level string, ago time.Duration, extra map[string]string) {
		r := victoria.Record{
			"_time": now.Add(-ago).UTC().Format(time.RFC3339Nano), "_msg": msg,
			"shed.project.id": prj.ID, "shed.service.id": id, "shed.source": source,
		}
		if level != "" {
			r["level"] = level
		}
		for k, v := range extra {
			r[k] = v
		}
		recs = append(recs, r)
	}
	levels := []string{"info", "warn", "error"}
	for i := range 60 {
		word := "plain"
		if i%3 == 0 {
			word = "needle"
		}
		rec(svc.ID, "container", fmt.Sprintf("A line %02d %s", i, word), levels[i%3], time.Duration(i+10)*5*time.Second, map[string]string{"stream": "stdout"})
	}
	for i := range 20 {
		rec(other.ID, "container", fmt.Sprintf("B line %02d needle", i), "error", time.Duration(i+10)*5*time.Second, nil)
	}
	rec(svc.ID, "container", "A leak "+secretValue+" here", "error", 3*time.Second, nil)
	for i, d := range []string{"0.010", "0.020", "0.500", "2.250"} {
		rec(svc.ID, "edge", "GET /p"+d, "", time.Duration(i+1)*time.Second, map[string]string{
			"method": "GET", "host": "web.example.com", "path": "/p" + d, "status": "200", "duration": d, "trace_id": strings.Repeat("ab", 16),
		})
	}
	if err := vl.InsertJSONLines(ctx, recs, victoria.InsertOptions{
		StreamFields: []string{"shed.project.id", "shed.service.id", "shed.source"}, MsgField: "_msg", TimeField: "_time",
	}); err != nil {
		t.Fatal(err)
	}

	type record struct {
		Time    time.Time
		Level   string
		Message string
		Stream  string
	}
	type page struct {
		Records []record
		Next    string
	}
	history := func(q url.Values) page {
		var p page
		get("/api/services/"+svc.ID+"/logs/history?"+q.Encode(), &p)
		return p
	}

	t.Run("log history", func(t *testing.T) {
		eventually("log records", func() bool {
			return len(history(url.Values{"q": {"needle"}, "limit": {"500"}}).Records) == 20
		})
		var all []record
		q := url.Values{"q": {"needle"}, "limit": {"7"}}
		pages := 0
		for {
			p := history(q)
			pages++
			all = append(all, p.Records...)
			if p.Next == "" {
				break
			}
			q.Set("before", p.Next)
			if pages > 10 {
				t.Fatal("paging does not end")
			}
		}
		if len(all) != 20 || pages != 3 {
			t.Fatalf("%d records in %d pages, want 20 in 3", len(all), pages)
		}
		seen := map[string]bool{}
		for i, r := range all {
			if !strings.HasPrefix(r.Message, "A line") || !strings.HasSuffix(r.Message, "needle") || seen[r.Message] {
				t.Errorf("record %d = %+v", i, r)
			}
			seen[r.Message] = true
			if i > 0 && r.Time.After(all[i-1].Time) {
				t.Errorf("record %d is newer than the one before", i)
			}
		}
		if all[0].Stream != "stdout" {
			t.Errorf("stream = %q", all[0].Stream)
		}
		for _, level := range []string{"info", "warn", "error"} {
			p := history(url.Values{"level": {level}, "limit": {"500"}})
			for _, r := range p.Records {
				if r.Level != level {
					t.Errorf("level %s returned %+v", level, r)
				}
			}
			if len(p.Records) < 20 {
				t.Errorf("level %s: %d records", level, len(p.Records))
			}
		}
		if p := history(url.Values{"q": {`"line 03" level:info`}, "limit": {"500"}}); len(p.Records) != 1 {
			t.Errorf("phrase and field search found %d records", len(p.Records))
		}
		// Searches that try to leave the service or add syntax find nothing
		// of the neighbor's and never fail the request.
		for _, attack := range []string{
			`needle" OR "B`, `needle) OR (shed.service.id:` + other.ID, `needle | delete`, `_stream:{"shed.service.id"="` + other.ID + `"}`,
			`shed.service.id:` + other.ID, `* | limit 1000`, `needle or B`, `{shed.service.id="` + other.ID + `"}`, "line\n| drop _msg",
		} {
			var p page
			resp := get("/api/services/"+svc.ID+"/logs/history?"+url.Values{"q": {attack}, "limit": {"500"}}.Encode(), nil)
			if resp.StatusCode == http.StatusOK {
				p = history(url.Values{"q": {attack}, "limit": {"500"}})
			} else if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("search %q = %s", attack, resp.Status)
			}
			for _, r := range p.Records {
				if strings.HasPrefix(r.Message, "B ") {
					t.Errorf("search %q returned the neighbor's record %+v", attack, r)
				}
			}
		}
		if p := history(url.Values{"q": {`needle or B`}}); len(p.Records) != 0 {
			t.Errorf("'needle or B' matched %d records; 'or' must be a plain word", len(p.Records))
		}
	})

	// Traces.
	traceID := strings.Repeat("ab", 16)
	tid := make([]byte, 16)
	for i := range tid {
		fmt.Sscanf(traceID[i*2:i*2+2], "%02x", &tid[i])
	}
	start := uint64(now.Add(-2 * time.Minute).UnixNano())
	str := func(k, v string) *commonpb.KeyValue {
		return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
	}
	body, err := proto.Marshal(&coltrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", "web"), str("shed.service.id", svc.ID)}},
		ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{
			{TraceId: tid, SpanId: []byte{1, 1, 1, 1, 1, 1, 1, 1}, Name: "GET /slow", Kind: tracepb.Span_SPAN_KIND_SERVER,
				StartTimeUnixNano: start, EndTimeUnixNano: start + uint64(300*time.Millisecond), Attributes: []*commonpb.KeyValue{str("http.method", "GET")}},
			{TraceId: tid, SpanId: []byte{2, 2, 2, 2, 2, 2, 2, 2}, ParentSpanId: []byte{1, 1, 1, 1, 1, 1, 1, 1}, Name: "db", Kind: tracepb.Span_SPAN_KIND_CLIENT,
				StartTimeUnixNano: start + uint64(10*time.Millisecond), EndTimeUnixNano: start + uint64(200*time.Millisecond),
				Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR}},
		}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := vt.ForwardOTLP(ctx, body); err != nil {
		t.Fatal(err)
	}

	// The MCP tools, through the handler.
	mcpSrv := httptest.NewServer(mcpHandler)
	defer mcpSrv.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "it", Version: "1"}, nil).Connect(ctx,
		&sdk.StreamableClientTransport{Endpoint: mcpSrv.URL, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tool := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return b.String(), res.IsError
	}

	t.Run("mcp", func(t *testing.T) {
		text, isErr := tool("telemetry_status", nil)
		if isErr || !strings.Contains(text, `"enabled":true`) || strings.Count(text, `"state":"running"`) != 4 {
			t.Errorf("telemetry_status: %v %s", isErr, text)
		}

		var metricsOut struct {
			ResultType string `json:"resultType"`
			Series     []struct {
				Labels map[string]string `json:"labels"`
				Values [][2]*float64     `json:"values"`
			} `json:"series"`
		}
		eventually("query_metrics", func() bool {
			text, isErr := tool("query_metrics", map[string]any{
				"query": fmt.Sprintf(`shed_container_memory_bytes{shed_service_id=%q}`, svc.ID), "start": "30m", "step": "30s",
			})
			if isErr {
				t.Fatalf("query_metrics: %s", text)
			}
			metricsOut.Series = nil
			if err := json.Unmarshal([]byte(text), &metricsOut); err != nil {
				t.Fatal(err)
			}
			return len(metricsOut.Series) == 1 && len(metricsOut.Series[0].Values) > 0
		})
		if l := metricsOut.Series[0].Labels; metricsOut.ResultType != "matrix" || l["shed_service_name"] != "web" || l["shed_project_name"] != "acme" || l["shed_source"] != "sampler" {
			t.Errorf("query_metrics = %+v", metricsOut)
		}
		for _, v := range metricsOut.Series[0].Values {
			if v[1] == nil || *v[1] < 100<<20 {
				t.Errorf("value %v", v)
			}
		}
		if text, isErr := tool("query_metrics", map[string]any{"query": "0/0"}); isErr || !json.Valid([]byte(text)) {
			t.Errorf("query_metrics 0/0: %v %s", isErr, text)
		} else {
			t.Logf("0/0 answers %s", text)
		}
		if text, isErr := tool("query_metrics", map[string]any{"query": "sum("}); !isErr || strings.Contains(text, "internal error") {
			t.Errorf("a bad query: %v %q", isErr, text)
		}

		text, isErr = tool("query_logs", map[string]any{"query": fmt.Sprintf(`_stream:{"shed.service.id"=%q} leak | limit 5`, svc.ID), "start": "1h"})
		if isErr || !strings.Contains(text, "A leak *** here") || strings.Contains(text, secretValue) {
			t.Errorf("query_logs did not mask: %v %s", isErr, text)
		}
		text, isErr = tool("error_logs", map[string]any{"service_id": svc.ID, "since": "1h", "limit": 500})
		if isErr || !strings.Contains(text, `"level":"error"`) || strings.Contains(text, `"level":"info"`) || strings.Contains(text, "B line") || strings.Contains(text, secretValue) {
			t.Errorf("error_logs: %v %.300s", isErr, text)
		}
		text, isErr = tool("slow_requests", map[string]any{"service_id": svc.ID, "limit": 2})
		var slow struct {
			P99Ms    float64 `json:"p99Ms"`
			Requests []struct {
				Path       string  `json:"path"`
				DurationMs float64 `json:"durationMs"`
			} `json:"requests"`
		}
		if err := json.Unmarshal([]byte(text), &slow); isErr || err != nil || len(slow.Requests) != 2 ||
			slow.Requests[0].Path != "/p2.250" || slow.Requests[0].DurationMs != 2250 || slow.P99Ms < 500 {
			t.Errorf("slow_requests: %v %v %s", isErr, err, text)
		}

		var found struct {
			Traces []struct {
				TraceID string `json:"traceId"`
				Name    string `json:"name"`
				Spans   int    `json:"spans"`
				Errors  int    `json:"errors"`
			} `json:"traces"`
		}
		eventually("search_traces", func() bool {
			text, isErr := tool("search_traces", map[string]any{"service_id": svc.ID, "min_duration": "100ms", "start": "1h"})
			found.Traces = nil
			return !isErr && json.Unmarshal([]byte(text), &found) == nil && len(found.Traces) == 1
		})
		if f := found.Traces[0]; f.TraceID != traceID || f.Name != "GET /slow" || f.Spans != 2 || f.Errors != 1 {
			t.Errorf("search_traces = %+v", f)
		}
		eventually("get_trace", func() bool {
			text, isErr = tool("get_trace", map[string]any{"trace_id": traceID})
			return !isErr
		})
		if isErr || !strings.Contains(text, `"name":"db"`) || !strings.Contains(text, `"status":"error"`) {
			t.Errorf("get_trace: %v %s", isErr, text)
		}
		eventually("list_dashboards", func() bool {
			text, isErr = tool("list_dashboards", map[string]any{"folder_uid": grafana.ServiceFolder(svc.ID)})
			return !isErr && strings.Contains(text, `"uid":"svc-overview-`+svc.ID+`"`)
		})
		if want := `"url":"` + shed.URL + `/grafana/d/svc-overview-` + svc.ID + `"`; !strings.Contains(text, want) || !strings.Contains(text, `"folder":"web"`) {
			t.Errorf("list_dashboards: %s, want %s in folder web", text, want)
		}
	})

	t.Run("trace link", func(t *testing.T) {
		resp := get("/api/telemetry/trace/"+traceID, nil)
		loc := resp.Header.Get("Location")
		if resp.StatusCode != http.StatusFound || !strings.HasPrefix(loc, "/grafana/explore?") {
			t.Fatalf("redirect: %s to %q", resp.Status, loc)
		}
		// Grafana's own data source answers the query the link carries.
		u, _ := url.Parse(loc)
		var panes map[string]struct {
			Queries []map[string]any `json:"queries"`
		}
		if err := json.Unmarshal([]byte(u.Query().Get("panes")), &panes); err != nil {
			t.Fatal(err)
		}
		var q map[string]any
		for _, p := range panes {
			q = p.Queries[0]
		}
		pw, err := st.Setting(ctx, "grafana_admin_password")
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]any{"queries": []any{q}, "from": "now-1h", "to": "now"})
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addrs.Grafana+"/grafana/api/ds/query", strings.NewReader(string(payload)))
		req.SetBasicAuth(grafana.AdminUser, pw)
		req.Header.Set("Content-Type", "application/json")
		eventually("Grafana to read the trace", func() bool {
			resp, err := http.DefaultClient.Do(req.Clone(ctx))
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			var out struct {
				Results map[string]struct {
					Status int
					Error  string
					Frames []struct {
						Data struct {
							Values [][]any `json:"values"`
						} `json:"data"`
					}
				}
			}
			json.NewDecoder(resp.Body).Decode(&out)
			a := out.Results["A"]
			if a.Error != "" {
				t.Logf("ds/query: %s", a.Error)
			}
			return resp.StatusCode == 200 && len(a.Frames) > 0 && len(a.Frames[0].Data.Values) > 0 && len(a.Frames[0].Data.Values[0]) == 2
		})
	})
}
