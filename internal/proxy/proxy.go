// Package proxy runs an embedded Caddy server that terminates TLS and routes
// requests by host name to upstream services.
//
// Caddy keeps its state in process-global variables, so a process can run only
// one Proxy.
package proxy

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/standard" // Registers the HTTP app and reverse_proxy.
	_ "github.com/caddyserver/caddy/v2/modules/caddypki"           // Registers the pki app.
	_ "github.com/caddyserver/caddy/v2/modules/caddytls"           // Registers TLS automation and issuers.
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"        // Registers file_system storage.
	_ "github.com/caddyserver/caddy/v2/modules/logging"            // Registers log writers.
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
}

// Proxy is an embedded reverse proxy.
type Proxy struct {
	cfg Config

	mu   sync.Mutex
	last []byte // Config JSON currently loaded, or nil.
}

// New returns a Proxy that is not yet running. Apply starts it.
func New(cfg Config) *Proxy {
	return &Proxy{cfg: cfg}
}

// Apply makes the proxy serve exactly routes, starting it if necessary.
// Certificates are obtained automatically for the route hosts. Apply does
// nothing if the resulting configuration is already loaded. It is safe for
// concurrent use.
func (p *Proxy) Apply(routes []Route) error {
	cfg, err := buildConfig(p.cfg, routes)
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if bytes.Equal(cfg, p.last) {
		return nil
	}
	if err := caddy.Load(cfg, false); err != nil {
		return fmt.Errorf("proxy: load config: %w", err)
	}
	p.last = cfg
	return nil
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
