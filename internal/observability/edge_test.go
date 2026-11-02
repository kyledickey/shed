package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/victoria"
)

type edgeStore struct {
	mu       sync.Mutex
	domains  []store.Domain
	services []store.Service
	projects []store.Project
}

func (s *edgeStore) AllDomains(context.Context) ([]store.Domain, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.Domain(nil), s.domains...), nil
}

func (s *edgeStore) AllServices(context.Context) ([]store.Service, error) {
	return s.services, nil
}

func (s *edgeStore) Projects(context.Context) ([]store.Project, error) {
	return s.projects, nil
}

type edgeLogs struct {
	mu      sync.Mutex
	records []victoria.Record
	streams []string
}

func (l *edgeLogs) InsertJSONLines(_ context.Context, records []victoria.Record, opts victoria.InsertOptions) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, records...)
	l.streams = opts.StreamFields
	return nil
}

// wait returns the records once there are n.
func (l *edgeLogs) wait(t *testing.T, n int) []victoria.Record {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		got := append([]victoria.Record(nil), l.records...)
		l.mu.Unlock()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("shipped %d records, want %d", len(got), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// edgeFixture runs an Edge over a store with one app, web.example.com, in
// project "shop", and records its metrics with a manual reader.
type edgeFixture struct {
	edge   *Edge
	store  *edgeStore
	logs   *edgeLogs
	reader *sdkmetric.ManualReader
}

func newEdgeFixture(t *testing.T, mask func(string) string) *edgeFixture {
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	if mask == nil {
		mask = func(s string) string { return s }
	}
	f := &edgeFixture{
		store: &edgeStore{
			domains:  []store.Domain{{ServiceID: "s1", Host: "web.example.com"}},
			services: []store.Service{{ID: "s1", ProjectID: "p1", Name: "web", Kind: "app"}, {ID: "s2", ProjectID: "p1", Name: "api", Kind: "app"}},
			projects: []store.Project{{ID: "p1", Name: "shop"}},
		},
		logs:   &edgeLogs{},
		reader: reader,
	}
	f.edge = NewEdge(EdgeConfig{
		Store:         f.store,
		Logs:          f.logs,
		Mask:          func(context.Context) (func(string) string, error) { return mask, nil },
		DashboardHost: "Shed.Example.com",
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return f
}

// run starts the edge's worker and waits until it takes entries.
func (f *edgeFixture) run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.edge.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	for !f.edge.running.Load() {
		time.Sleep(time.Millisecond)
	}
}

// points returns the data points of a counter, each as its attributes.
func (f *edgeFixture) points(t *testing.T, name string) []map[string]any {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			add := func(set attribute.Set, v any) {
				p := map[string]any{"value": v}
				for _, kv := range set.ToSlice() {
					p[string(kv.Key)] = kv.Value.AsString()
				}
				out = append(out, p)
			}
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					add(dp.Attributes, dp.Value)
				}
			case metricdata.Histogram[float64]:
				for _, dp := range d.DataPoints {
					add(dp.Attributes, dp.Count)
				}
			}
		}
	}
	return out
}

// find returns the point whose attributes include want.
func find(points []map[string]any, want map[string]any) map[string]any {
	for _, p := range points {
		ok := true
		for k, v := range want {
			if p[k] != v {
				ok = false
			}
		}
		if ok {
			return p
		}
	}
	return nil
}

func entry(host, method, path string, status int) proxy.AccessEntry {
	return proxy.AccessEntry{
		Time:   time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
		Method: method, Host: host, Path: path, Status: status,
		Duration: 150 * time.Millisecond, BytesIn: 10, BytesOut: 2048,
	}
}

func TestEdgeAttributesRequestsToServices(t *testing.T) {
	f := newEdgeFixture(t, nil)
	f.run(t)
	f.edge.Handle(entry("web.example.com", "GET", "/", 200))
	f.edge.Handle(entry("web.example.com", "BREW", "/", 405))
	f.edge.Handle(entry("shed.example.com", "POST", "/api/x", 500))
	f.edge.Handle(entry("scanner.example.net", "GET", "/.env", 404))

	reqs := f.points(t, "shed_http_requests_total")
	for _, want := range []map[string]any{
		{"shed_service_id": "s1", "shed_service_name": "web", "shed_service_kind": "app", "shed_project_id": "p1", "shed_project_name": "shop",
			"shed_source": "edge", "host": "web.example.com", "method": "GET", "code": "200", "value": int64(1)},
		{"shed_service_id": "s1", "method": "other", "code": "405"},
		{"shed_service_id": "shed", "shed_service_kind": "shed", "shed_project_id": "", "host": "shed.example.com", "code": "500"},
		{"shed_service_id": "", "host": "unrouted", "code": "404"},
	} {
		if find(reqs, want) == nil {
			t.Errorf("no shed_http_requests_total point with %v in %v", want, reqs)
		}
	}
	if p := find(f.points(t, "shed_http_request_duration_seconds"), map[string]any{"shed_service_id": "s1", "host": "web.example.com", "code": "200"}); p == nil || p["method"] != nil {
		t.Errorf("duration point for s1 = %v, want one without a method label", p)
	}
	if p := find(f.points(t, "shed_http_response_bytes_total"), map[string]any{"shed_service_id": "s1", "host": "web.example.com"}); p == nil || p["value"] != int64(2*2048) {
		t.Errorf("response bytes for s1 = %v, want 4096", p)
	}

	recs := f.logs.wait(t, 4)
	byPath := map[string]victoria.Record{}
	for _, r := range recs {
		if r["method"] != "BREW" {
			byPath[r["path"]] = r
		}
	}
	web := byPath["/"]
	if web["shed.service.id"] != "s1" || web["shed.project.name"] != "shop" || web["shed.source"] != "edge" ||
		web["status"] != "200" || web["duration"] != "0.15" || web["bytes_out"] != "2048" ||
		web["_time"] != "2026-10-27T12:00:00Z" {
		t.Errorf("record for web.example.com = %v", web)
	}
	if r := byPath["/.env"]; r["shed.service.id"] != "" || r["host"] != "scanner.example.net" {
		t.Errorf("record for an unrouted host = %v", r)
	}
	if got := strings.Join(f.logs.streams, ","); got != "shed.project.id,shed.service.id,shed.source" {
		t.Errorf("stream fields = %s", got)
	}
}

func TestEdgeRefreshPicksUpNewDomains(t *testing.T) {
	f := newEdgeFixture(t, nil)
	f.run(t)
	f.store.mu.Lock()
	f.store.domains = append(f.store.domains, store.Domain{ServiceID: "s2", Host: "api.example.com"})
	f.store.mu.Unlock()
	f.edge.Refresh()

	deadline := time.Now().Add(5 * time.Second)
	for find(f.points(t, "shed_http_requests_total"), map[string]any{"shed_service_id": "s2", "host": "api.example.com"}) == nil {
		if time.Now().After(deadline) {
			t.Fatal("api.example.com was not attributed to s2 after Refresh")
		}
		f.edge.Handle(entry("api.example.com", "GET", "/", 200))
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEdgeMasksRecords(t *testing.T) {
	const secret = "s3cr3t-api-key"
	f := newEdgeFixture(t, func(s string) string { return strings.ReplaceAll(s, secret, "***") })
	f.run(t)
	tests := []struct {
		path, wantPath string
	}{
		{"/hooks/" + secret, "/hooks/***"},
		{"/hooks/s3cr3t%2Dapi%2Dkey", "/hooks/***"},
		{"/plain%20path", "/plain%20path"},
	}
	for _, tt := range tests {
		f.edge.Handle(entry("web.example.com", "GET", tt.path, 200))
	}
	recs := f.logs.wait(t, len(tests))
	for i, tt := range tests {
		r := recs[i]
		if r["path"] != tt.wantPath || strings.Contains(r["_msg"], secret) {
			t.Errorf("record for %s: path %q, msg %q; want %q", tt.path, r["path"], r["_msg"], tt.wantPath)
		}
		for _, field := range []string{"client_ip", "query", "user_agent", "referer"} {
			if _, ok := r[field]; ok {
				t.Errorf("record for %s has %s: %v", tt.path, field, r)
			}
		}
	}
}

func TestEdgeDropsWhenQueueIsFull(t *testing.T) {
	f := newEdgeFixture(t, nil)
	// No worker drains the queue.
	f.edge.hosts.Store(&map[string]telemetry.Service{"web.example.com": {ID: "s1", Name: "web", Kind: "app"}})
	f.edge.running.Store(true)
	for range edgeQueue + 3 {
		f.edge.Handle(entry("web.example.com", "GET", "/", 200))
	}
	p := find(f.points(t, "shed_telemetry_dropped_total"), map[string]any{"shed_service_id": "s1", "signal": "logs", "reason": "edge_queue_full"})
	if p == nil || p["value"] != int64(3) {
		t.Errorf("dropped = %v, want 3", p)
	}
	if p := find(f.points(t, "shed_http_requests_total"), map[string]any{"shed_service_id": "s1"}); p == nil || p["value"] != int64(edgeQueue+3) {
		t.Errorf("requests = %v, want every request counted", p)
	}
}

func TestEdgeIgnoresEntriesWhileStopped(t *testing.T) {
	f := newEdgeFixture(t, nil)
	f.edge.Handle(entry("web.example.com", "GET", "/", 200))
	if p := f.points(t, "shed_http_requests_total"); len(p) != 0 {
		t.Errorf("recorded %v before Run", p)
	}
}
