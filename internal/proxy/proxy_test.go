package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// TestApply runs a real Caddy on loopback ports. Caddy has process-global
// state, so this is the only test that starts it.
func TestApply(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from upstream, host=%s", r.Host)
	}))
	defer upstream.Close()

	cfg := Config{HTTPPort: freePort(t), HTTPSPort: freePort(t), StorageDir: t.TempDir()}
	p := New(cfg)
	t.Cleanup(func() { p.Stop() })

	routes := []Route{{Host: "localhost", Upstream: strings.TrimPrefix(upstream.URL, "http://")}}
	if err := p.Apply(routes); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := p.Apply(routes); err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}

	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
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
