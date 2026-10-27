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
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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
		fmt.Fprintf(w, "hello from upstream, host=%s, xfh=%s, xff=%s", r.Host, r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-For"))
	}))
	defer upstream.Close()

	// Cloudflare is on so Caddy validates the trusted proxy config. Loopback
	// clients are not in its ranges, so requests here are unaffected.
	reader := sdkmetric.NewManualReader()
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	cfg := Config{HTTPPort: freePort(t), HTTPSPort: freePort(t), StorageDir: t.TempDir(), Cloudflare: true}
	p := New(cfg)
	t.Cleanup(func() { p.Stop() })

	routes := []Route{{Host: "localhost", Upstream: strings.TrimPrefix(upstream.URL, "http://")}}
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
