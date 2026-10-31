package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

// collector is an httptest OTLP/HTTP endpoint that decodes what it receives.
type collector struct {
	*httptest.Server
	requests atomic.Int64

	mu        sync.Mutex
	resources map[string][]*resourcepb.Resource
	spans     []string
	metrics   []string
	logs      []string
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{resources: map[string][]*resourcepb.Resource{}}
	c.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.requests.Add(1)
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		defer c.mu.Unlock()
		switch r.URL.Path {
		case "/v1/traces":
			var req coltrace.ExportTraceServiceRequest
			if proto.Unmarshal(body, &req) == nil {
				for _, rs := range req.ResourceSpans {
					c.resources["traces"] = append(c.resources["traces"], rs.Resource)
					for _, ss := range rs.ScopeSpans {
						for _, s := range ss.Spans {
							c.spans = append(c.spans, s.Name)
						}
					}
				}
			}
		case "/v1/metrics":
			var req colmetrics.ExportMetricsServiceRequest
			if proto.Unmarshal(body, &req) == nil {
				for _, rm := range req.ResourceMetrics {
					c.resources["metrics"] = append(c.resources["metrics"], rm.Resource)
					for _, sm := range rm.ScopeMetrics {
						for _, m := range sm.Metrics {
							c.metrics = append(c.metrics, m.Name)
						}
					}
				}
			}
		case "/v1/logs":
			var req collogs.ExportLogsServiceRequest
			if proto.Unmarshal(body, &req) == nil {
				for _, rl := range req.ResourceLogs {
					c.resources["logs"] = append(c.resources["logs"], rl.Resource)
					for _, sl := range rl.ScopeLogs {
						for _, lr := range sl.LogRecords {
							c.logs = append(c.logs, lr.Body.GetStringValue())
						}
					}
				}
			}
		}
	}))
	t.Cleanup(c.Close)
	return c
}

func newProviders(t *testing.T, endpoint string) *Providers {
	t.Helper()
	p, err := New(context.Background(), Config{Endpoint: endpoint, Version: "v1.2.3", Host: "box"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// emit produces one span, one custom metric, and one log record.
func emit(ctx context.Context, p *Providers) {
	ctx, span := p.TracerProvider().Tracer("test").Start(ctx, "work")
	span.End()
	c, _ := p.MeterProvider().Meter("test").Int64Counter("shed_test_total")
	c.Add(ctx, 1)
	slog.New(NewSlogHandler(slog.NewTextHandler(io.Discard, nil), p.LoggerProvider())).InfoContext(ctx, "hello")
}

func TestDisabledExportsNothing(t *testing.T) {
	c := newCollector(t)
	p := newProviders(t, c.URL)

	emit(context.Background(), p)
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := c.requests.Load(); n != 0 {
		t.Fatalf("collector got %d requests while disabled", n)
	}
}

func TestEnabledExportsAllSignals(t *testing.T) {
	c := newCollector(t)
	p := newProviders(t, c.URL+"/")
	p.SetEnabled(true)

	emit(context.Background(), p)
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.spans) != 1 || c.spans[0] != "work" {
		t.Errorf("spans = %v, want [work]", c.spans)
	}
	if !contains(c.metrics, "shed_test_total") || !contains(c.metrics, "go.goroutine.count") {
		t.Errorf("metrics = %v, want shed_test_total and go.goroutine.count", c.metrics)
	}
	if len(c.logs) != 1 || c.logs[0] != "hello" {
		t.Errorf("logs = %v, want [hello]", c.logs)
	}

	want := map[string]string{
		"service.name":      "shed",
		"service.version":   "v1.2.3",
		"host.name":         "box",
		"shed.service.id":   "shed",
		"shed.service.kind": "shed",
		"shed.source":       "shed",
	}
	for _, signal := range []string{"traces", "metrics", "logs"} {
		rs := c.resources[signal]
		if len(rs) == 0 {
			t.Errorf("%s: no resource received", signal)
			continue
		}
		got := attrMap(rs[0].Attributes)
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: resource %s = %q, want %q", signal, k, got[k], v)
			}
		}
	}
}

func TestToggleStopsExport(t *testing.T) {
	c := newCollector(t)
	p := newProviders(t, c.URL)
	ctx := context.Background()

	p.SetEnabled(true)
	emit(ctx, p)
	flush(t, p)
	p.SetEnabled(false)
	emit(ctx, p)
	if err := p.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.spans) != 1 || len(c.logs) != 1 {
		t.Errorf("spans = %v, logs = %v; want one of each, from while enabled", c.spans, c.logs)
	}
}

func TestToggleRace(t *testing.T) {
	c := newCollector(t)
	p := newProviders(t, c.URL)
	ctx := context.Background()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					emit(ctx, p)
				}
			}
		}()
	}
	for i := range 200 {
		p.SetEnabled(i%2 == 0)
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func flush(t *testing.T, p *Providers) {
	t.Helper()
	ctx := context.Background()
	for _, err := range []error{p.tp.ForceFlush(ctx), p.mp.ForceFlush(ctx), p.lp.ForceFlush(ctx)} {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func contains(s []string, v string) bool {
	for _, e := range s {
		if e == v {
			return true
		}
	}
	return false
}

func attrMap(kvs []*commonpb.KeyValue) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.GetStringValue()
	}
	return m
}
