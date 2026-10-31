// Package proxy runs an embedded Caddy server that terminates TLS and routes
// requests by host name to upstream services.
//
// Caddy keeps its state in process-global variables, so a process can run only
// one Proxy.
package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/standard" // Registers the HTTP app and reverse_proxy.
	_ "github.com/caddyserver/caddy/v2/modules/caddypki"           // Registers the pki app.
	_ "github.com/caddyserver/caddy/v2/modules/caddytls"           // Registers TLS automation and issuers.
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"        // Registers file_system storage.
	_ "github.com/caddyserver/caddy/v2/modules/logging"            // Registers log writers.
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Route sends requests for a host name to an upstream.
type Route struct {
	// Host is the host name to match, such as "app.example.com".
	Host string
	// Upstream is the address to proxy to, as host:port.
	Upstream string
}

// Config configures a Proxy.
type Config struct {
	// HTTPPort is the port for plain HTTP, which serves ACME challenges and
	// redirects to HTTPS.
	HTTPPort int
	// HTTPSPort is the port the proxy serves HTTPS on.
	HTTPSPort int
	// Email is the optional contact address for ACME certificates.
	Email string
	// StorageDir is where certificates are stored. Empty uses Caddy's default.
	StorageDir string
	// LogFile is the file Caddy logs to. Caddy rotates it itself. Empty
	// discards Caddy's logs.
	LogFile string
	// Cloudflare trusts requests from Cloudflare's edge to carry the client
	// IP in CF-Connecting-IP and X-Forwarded-* headers. Requests from other
	// addresses are unaffected.
	Cloudflare bool
	// Access receives an entry for every request while telemetry is on. It
	// runs on the request's goroutine, after the response, so it must not
	// block.
	Access func(AccessEntry)
	// TracesURL is the OTLP/HTTP endpoint Caddy exports its spans to while
	// telemetry is on, such as "http://127.0.0.1:4318/v1/traces".
	TracesURL string
}

// Proxy is an embedded reverse proxy.
type Proxy struct {
	cfg Config

	mu        sync.Mutex
	last      []byte  // Config JSON currently loaded, or nil.
	routes    []Route // Routes last applied; guarded by mu.
	telemetry bool    // Access logs and tracing are on; guarded by mu.

	applyDuration metric.Float64Histogram
}

// New returns a Proxy that is not yet running. Apply starts it.
func New(cfg Config) *Proxy {
	// Creating an instrument fails only for an invalid name, and the
	// instrument returned works regardless.
	d, _ := otel.Meter("github.com/kyledickey/shed/internal/proxy").Float64Histogram("shed_route_apply_duration_seconds",
		metric.WithDescription("Duration of loading a changed route set into Caddy."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10))
	if cfg.Access != nil {
		accessSink.Store(&cfg.Access)
	}
	return &Proxy{cfg: cfg, applyDuration: d}
}

// Apply makes the proxy serve exactly routes, starting it if necessary.
// Certificates are obtained automatically for the route hosts. Apply does
// nothing if the resulting configuration is already loaded. It is safe for
// concurrent use.
func (p *Proxy) Apply(routes []Route) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.load(routes, p.telemetry); err != nil {
		return err
	}
	p.routes = routes
	return nil
}

// SetTelemetry turns access entries and tracing on or off. If the proxy is
// running, it reloads the last applied routes; otherwise the next Apply
// starts it that way.
func (p *Proxy) SetTelemetry(on bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last != nil {
		if err := p.load(p.routes, on); err != nil {
			return err
		}
	}
	p.telemetry = on
	return nil
}

// load loads the configuration for routes unless it is loaded already.
// The caller holds mu.
func (p *Proxy) load(routes []Route, telemetry bool) error {
	cfg, err := buildConfig(p.cfg, routes, telemetry)
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	if bytes.Equal(cfg, p.last) {
		return nil
	}
	start := time.Now()
	if telemetry {
		err = withEnv(tracingEnv(p.cfg.TracesURL), func() error { return caddy.Load(cfg, false) })
	} else {
		err = caddy.Load(cfg, false)
	}
	p.applyDuration.Record(context.Background(), time.Since(start).Seconds())
	if err != nil {
		return fmt.Errorf("proxy: load config: %w", err)
	}
	p.last = cfg
	return nil
}

// tracingEnv configures the exporter of Caddy's tracing handler, which
// reads only the standard OpenTelemetry environment variables.
func tracingEnv(tracesURL string) map[string]string {
	return map[string]string{
		"OTEL_TRACES_EXPORTER":               "otlp",
		"OTEL_EXPORTER_OTLP_TRACES_PROTOCOL": "http/protobuf",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": tracesURL,
		"OTEL_PROPAGATORS":                   "tracecontext",
	}
}

// withEnv runs fn with vars set in the process environment and then
// restores it. Caddy's tracer reads them only while a config is provisioned,
// and keeping them set would hand them to every process shed starts, such
// as builds.
func withEnv(vars map[string]string, fn func() error) error {
	prev := make(map[string]*string, len(vars))
	for k, v := range vars {
		if old, ok := os.LookupEnv(k); ok {
			prev[k] = &old
		} else {
			prev[k] = nil
		}
		os.Setenv(k, v)
	}
	defer func() {
		for k, old := range prev {
			if old == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *old)
			}
		}
	}()
	return fn()
}

// Stop shuts the proxy down. A later Apply starts it again.
func (p *Proxy) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = nil
	if err := caddy.Stop(); err != nil {
		return fmt.Errorf("proxy: stop: %w", err)
	}
	return nil
}
