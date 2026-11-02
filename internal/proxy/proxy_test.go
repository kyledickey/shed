package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// spoofTransport sends forwarding headers that only a trusted proxy may set.
type spoofTransport struct{ http.RoundTripper }

func (s spoofTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	r.Header.Set("CF-Connecting-IP", "198.51.100.2")
	r.Header.Set("X-Forwarded-Host", "evil.example")
	return s.RoundTripper.RoundTrip(r)
}

// TestApply runs a real Caddy on loopback ports. Caddy has process-global
// state, so this is the only test that starts it.
func TestApply(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from upstream, host=%s, xfh=%s, tp=%s, xff=%s", r.Host, r.Header.Get("X-Forwarded-Host"), r.Header.Get("Traceparent"), r.Header.Get("X-Forwarded-For"))
	}))
	defer upstream.Close()

	// Cloudflare is on so Caddy validates the trusted proxy config. Loopback
	// clients are not in its ranges, so requests here are unaffected.
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	collector := &spanCollector{}
	otlp := httptest.NewServer(collector)
	defer otlp.Close()
	entries := make(chan AccessEntry, 16)
	cfg := Config{
		HTTPPort: freePort(t), HTTPSPort: freePort(t), StorageDir: t.TempDir(), Cloudflare: true,
		Access:    func(e AccessEntry) { entries <- e },
		TracesURL: otlp.URL + "/v1/traces",
		LogFile:   filepath.Join(t.TempDir(), "caddy.log"),
	}
	p := New(cfg)
	t.Cleanup(func() { p.Stop() })

	routes := []Route{
		{Host: "localhost", Upstream: strings.TrimPrefix(upstream.URL, "http://")},
		// Not 127.0.0.1, which the log must not mention: the error names the upstream.
		{Host: "dead.localhost", Upstream: fmt.Sprintf("127.0.0.2:%d", freePort(t))},
	}
	if err := p.Apply(routes); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := p.Apply(routes); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	if n := applyCount(t, reader); n != 1 {
		t.Errorf("shed_route_apply_duration_seconds count = %d, want 1 for one changed route set", n)
	}

	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     spoofTransport{&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func(url string) (int, string, http.Header) {
		t.Helper()
		var lastErr error
		for range 50 { // The internal CA may still be issuing the certificate.
			resp, err := client.Get(url)
			if err != nil {
				lastErr = err
				time.Sleep(100 * time.Millisecond)
				continue
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(body), resp.Header
		}
		t.Fatalf("GET %s: %v", url, lastErr)
		return 0, "", nil
	}

	status, body, _ := get(fmt.Sprintf("https://localhost:%d/", cfg.HTTPSPort))
	if status != http.StatusOK || !strings.HasPrefix(body, "hello from upstream") {
		t.Errorf("https routed request = %d %q", status, body)
	}
	// Loopback is not a Cloudflare address, so the spoofed headers are ignored.
	if strings.Contains(body, "evil.example") {
		t.Errorf("upstream got the spoofed X-Forwarded-Host: %q", body)
	}
	_, xff, _ := strings.Cut(body, "xff=")
	if ip, err := netip.ParseAddr(xff); err != nil || !ip.IsLoopback() {
		t.Errorf("upstream X-Forwarded-For = %q, want the loopback client", xff)
	}

	status, _, hdr := get(fmt.Sprintf("http://localhost:%d/x", cfg.HTTPPort))
	if status != http.StatusPermanentRedirect && status != http.StatusMovedPermanently && status != http.StatusFound {
		t.Errorf("http status = %d, want redirect", status)
	}
	// Caddy treats https_port as the public HTTPS port and omits it from redirects.
	if want := "https://localhost/x"; hdr.Get("Location") != want {
		t.Errorf("Location = %q, want %q", hdr.Get("Location"), want)
	}

	if len(entries) != 0 {
		t.Errorf("got %d access entries while telemetry is off", len(entries))
	}

	const queryValue = "query-sentinel"
	failUpstream := func() {
		t.Helper()
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/fail?token=%s", cfg.HTTPSPort, queryValue), nil)
		req.Host = "dead.localhost"
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Errorf("dead upstream status = %d, want 502", resp.StatusCode)
		}
	}
	// checkLog waits for the nth upstream error in caddy.log, then checks that
	// the file holds nothing that identifies the client.
	checkLog := func(n int) {
		t.Helper()
		var log string
		for range 50 {
			b, err := os.ReadFile(cfg.LogFile)
			if err != nil {
				t.Fatal(err)
			}
			log = string(b)
			if strings.Count(log, "dial tcp 127.0.0.2") >= n && strings.Contains(log, "tls-alpn challenge") {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if strings.Count(log, "dial tcp 127.0.0.2") < n || !strings.Contains(log, "tls-alpn challenge") {
			t.Fatalf("caddy.log lacks %d upstream errors and the ALPN error:\n%s", n, log)
		}
		for _, leak := range []string{"198.51.100.1", "198.51.100.2", "127.0.0.1", "::1", queryValue} {
			if strings.Contains(log, leak) {
				t.Errorf("caddy.log contains %q:\n%s", leak, log)
			}
		}
	}
	failUpstream()
	// Any client can make certmagic log its address this way: no challenge is pending.
	conn, err := tls.Dial("tcp", fmt.Sprintf("localhost:%d", cfg.HTTPSPort), &tls.Config{
		InsecureSkipVerify: true, ServerName: "localhost", NextProtos: []string{"acme-tls/1"},
	})
	if err == nil {
		conn.Close()
	}
	checkLog(1)

	t.Run("telemetry", func(t *testing.T) {
		if err := p.SetTelemetry(true); err != nil {
			t.Fatalf("SetTelemetry(true) error = %v", err)
		}
		if os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == cfg.TracesURL {
			t.Error("the tracer's endpoint stayed in the process environment, where builds would inherit it")
		}
		const traceID, parentID = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/edge?token=abc", cfg.HTTPSPort), nil)
		req.Header.Set("Traceparent", "00-"+traceID+"-"+parentID+"-01")
		req.Header.Set("Authorization", "Bearer secret-token")
		req.Header.Set("Cookie", "session=secret-cookie")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		// Caddy continues the incoming trace with a span of its own and
		// forwards that span as the upstream's parent.
		_, rest, _ := strings.Cut(string(body), "tp=")
		tp, _, _ := strings.Cut(rest, ",")
		if !strings.HasPrefix(tp, "00-"+traceID+"-") || strings.Contains(tp, parentID) {
			t.Errorf("upstream traceparent = %q, want trace %s with Caddy's span", tp, traceID)
		}

		e := receive(t, entries)
		if e.Method != "GET" || e.Host != "localhost" || e.Path != "/edge" ||
			e.Status != 200 || e.BytesOut != int64(len(body)) || e.Duration <= 0 ||
			e.TLSVersion != "TLS 1.3" || e.TraceID != traceID || time.Since(e.Time) > time.Minute {
			t.Errorf("access entry = %+v", e)
		}

		// Requests for hosts without a route are logged too.
		req, _ = http.NewRequest("GET", fmt.Sprintf("https://localhost:%d/.env", cfg.HTTPSPort), nil)
		req.Host = "Unknown.Example:443"
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
		if e := receive(t, entries); e.Host != "unknown.example" || e.Status != 404 || e.TraceID != "" {
			t.Errorf("unrouted access entry = %+v", e)
		}

		failUpstream()
		if e := receive(t, entries); e.Host != "dead.localhost" || e.Path != "/fail" || e.Status != http.StatusBadGateway {
			t.Errorf("failed access entry = %+v", e)
		}
		checkLog(2)

		// Turning telemetry off removes the tracing handlers, which flushes
		// Caddy's spans.
		if err := p.SetTelemetry(false); err != nil {
			t.Fatalf("SetTelemetry(false) error = %v", err)
		}
		get(fmt.Sprintf("https://localhost:%d/", cfg.HTTPSPort))
		if len(entries) != 0 {
			t.Errorf("got %d access entries after telemetry was turned off", len(entries))
		}
		span := collector.find(traceID)
		if span == nil {
			t.Fatal("no span exported for the traced request")
		}
		// The gateway attributes edge spans by these two attributes.
		if got := stringAttr(span.Attributes, "server.address"); got != "localhost" {
			t.Errorf("span server.address = %q, want localhost", got)
		}
		if got := stringAttr(span.resource, "webengine.name"); got != "Caddy" {
			t.Errorf("span resource webengine.name = %q, want Caddy", got)
		}
		if got := fmt.Sprintf("%x", span.ParentSpanId); got != parentID {
			t.Errorf("span parent = %s, want %s", got, parentID)
		}
		if span.Kind != tracepb.Span_SPAN_KIND_SERVER || span.Name != "GET" {
			t.Errorf("span kind %v name %q, want a server span named GET", span.Kind, span.Name)
		}
	})

	// Reload with the route removed.
	if err := p.Apply(nil); err != nil {
		t.Fatalf("Apply(nil) error = %v", err)
	}
}

func applyCount(t *testing.T, reader *sdkmetric.ManualReader) uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	var n uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if h, ok := m.Data.(metricdata.Histogram[float64]); ok && m.Name == "shed_route_apply_duration_seconds" {
				for _, dp := range h.DataPoints {
					n += dp.Count
				}
			}
		}
	}
	return n
}

func receive(t *testing.T, entries <-chan AccessEntry) AccessEntry {
	t.Helper()
	select {
	case e := <-entries:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("no access entry")
		return AccessEntry{}
	}
}

// spanCollector is an OTLP/HTTP traces endpoint that keeps every span.
type spanCollector struct {
	mu    sync.Mutex
	spans []collectedSpan
}

type collectedSpan struct {
	*tracepb.Span
	resource []*commonpb.KeyValue
}

func (c *spanCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req coltracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				c.spans = append(c.spans, collectedSpan{sp, rs.Resource.GetAttributes()})
			}
		}
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
}

func (c *spanCollector) find(traceID string) *collectedSpan {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.spans {
		if fmt.Sprintf("%x", s.TraceId) == traceID {
			return &s
		}
	}
	return nil
}

func stringAttr(kvs []*commonpb.KeyValue, key string) string {
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value.GetStringValue()
		}
	}
	return ""
}
