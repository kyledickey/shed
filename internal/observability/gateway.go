package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/firewall"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
)

// GatewayStore is what the gateway reads: names, domains, and caps.
// *store.Store implements it.
type GatewayStore interface {
	DirectoryStore
	AllTelemetryCaps(ctx context.Context) (map[string]store.TelemetryCaps, error)
}

var _ GatewayStore = (*store.Store)(nil)

// GatewayDocker finds service containers and project networks.
// *docker.Client implements it.
type GatewayDocker interface {
	ContainerLister
	Networks(ctx context.Context, prefix string) ([]docker.Network, error)
}

var _ GatewayDocker = (*docker.Client)(nil)

// Firewall opens the gateway's port to containers. *firewall.Firewall
// implements it.
type Firewall interface {
	Sync(ctx context.Context, rules []firewall.Rule) error
}

var _ Firewall = (*firewall.Firewall)(nil)

// Masks build the functions that mask secret values in telemetry.
// *control.Plane implements it.
type Masks interface {
	// ShedMask masks shed's own records: every saved variable value.
	ShedMask(ctx context.Context) (func(string) string, error)
	// TelemetryMask masks a project's records: the saved values of its
	// variables and, given a deployment, those its container started with.
	TelemetryMask(ctx context.Context, projectID, deploymentID string) (func(string) string, error)
}

// GatewayConfig configures a Gateway.
type GatewayConfig struct {
	Store  GatewayStore
	Docker GatewayDocker
	Masks  Masks
	// Settings returns the telemetry settings, for the default caps.
	Settings func(ctx context.Context) (Settings, error)
	// Port is telemetry.otlp_port, the port of every listener.
	Port int
	// Firewall, if set, keeps a rule per project bridge accepting TCP to
	// Port while the gateway runs, for hosts whose firewall drops input
	// from bridges.
	Firewall Firewall
	Log      *slog.Logger
}

// Timings of the gateway.
const (
	// networksEvery is how often listeners follow the project networks and
	// caps are reloaded.
	networksEvery = 5 * time.Second
	// directoryEvery is how often the container index is reloaded.
	directoryEvery = 30 * time.Second
	// missEvery bounds how often an unknown source reloads the index.
	missEvery = time.Second
)

// Gateway receives OTLP for shed: it runs a listener on every project
// network's gateway address and one on loopback, and tells an
// [otlp.Handler] who sent each request, how to mask it, and what the
// sender's caps are.
type Gateway struct {
	cfg    GatewayConfig
	listen func(addr string) (net.Listener, error)
	now    func() time.Time

	dir  atomic.Pointer[directory]
	caps atomic.Pointer[capsTable]

	loadMu sync.Mutex // serializes reloading the directory
	loaded time.Time  // when dir was last reloaded; guarded by loadMu

	firewallErr string // last firewall error logged; only Run uses it

	masksMu sync.Mutex
	masks   map[maskKey]func(string) string // cleared on reload; guarded by masksMu
}

// capsTable holds the default caps and every service's override.
type capsTable struct {
	defaults  Caps
	overrides map[string]store.TelemetryCaps
}

// maskKey names the values a record is masked with: a project's (all of
// them for shed) and those a deployment's container started with.
type maskKey struct{ projectID, deploymentID string }

var shedKey = maskKey{projectID: "*"}

func keyOf(src otlp.Source) maskKey {
	if src.ServiceID == shedSource.ServiceID {
		return shedKey
	}
	return maskKey{src.ProjectID, src.DeploymentID}
}

// NewGateway returns a Gateway. It listens only while Run runs.
func NewGateway(cfg GatewayConfig) *Gateway {
	return &Gateway{
		cfg:    cfg,
		listen: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		now:    time.Now,
		masks:  make(map[maskKey]func(string) string),
	}
}

// listenerKey is the context key of the listener a request arrived on.
type listenerKey struct{}

// listenerInfo describes a listener: a project network's, or loopback.
type listenerInfo struct {
	projectID string
	loopback  bool
	bridge    string // host interface of the project's network
}

// Run serves h on the listeners until ctx is done, following the project
// networks as they come and go. It is meant to run as a [Worker].
func (g *Gateway) Run(ctx context.Context, h http.Handler) {
	servers := make(map[string]*http.Server) // by address
	defer func() {
		for _, srv := range servers {
			srv.Close()
		}
		g.syncFirewall(context.WithoutCancel(ctx), nil)
	}()
	if err := g.reload(ctx); err != nil {
		g.cfg.Log.Warn("telemetry gateway: load containers", "err", err)
	}
	tick := time.NewTicker(networksEvery)
	defer tick.Stop()
	for {
		if err := g.loadCaps(ctx); err != nil {
			g.cfg.Log.Warn("telemetry gateway: load caps", "err", err)
		}
		want, err := g.listeners(ctx)
		if err != nil {
			g.cfg.Log.Warn("telemetry gateway: list networks", "err", err)
		} else {
			g.reconcile(servers, want, h)
			g.syncFirewall(ctx, want)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		g.loadMu.Lock()
		stale := g.now().Sub(g.loaded) >= directoryEvery
		g.loadMu.Unlock()
		if stale {
			if err := g.reload(ctx); err != nil {
				g.cfg.Log.Warn("telemetry gateway: load containers", "err", err)
			}
		}
	}
}

// listeners returns the addresses to listen on: loopback, and the gateway
// of every project's network.
func (g *Gateway) listeners(ctx context.Context) (map[string]listenerInfo, error) {
	port := strconv.Itoa(g.cfg.Port)
	want := map[string]listenerInfo{net.JoinHostPort("127.0.0.1", port): {loopback: true}}
	networks, err := g.cfg.Docker.Networks(ctx, networkPrefix)
	if err != nil {
		return nil, err
	}
	projects, err := g.cfg.Store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(projects))
	for _, p := range projects {
		known[p.ID] = true
	}
	for _, n := range networks {
		id := strings.TrimPrefix(n.Name, networkPrefix)
		if known[id] && n.Gateway.IsValid() {
			want[net.JoinHostPort(n.Gateway.String(), port)] = listenerInfo{projectID: id, bridge: n.Bridge}
		}
	}
	return want, nil
}

// reconcile opens the listeners in want that servers lacks and closes the
// ones it no longer has. A listener that fails to open is retried on the
// next call.
func (g *Gateway) reconcile(servers map[string]*http.Server, want map[string]listenerInfo, h http.Handler) {
	for addr, srv := range servers {
		if _, ok := want[addr]; !ok {
			srv.Close()
			delete(servers, addr)
			g.cfg.Log.Info("telemetry gateway: closed listener", "addr", addr)
		}
	}
	for addr, info := range want {
		if servers[addr] != nil {
			continue
		}
		ln, err := g.listen(addr)
		if err != nil {
			g.cfg.Log.Warn("telemetry gateway: listen", "addr", addr, "err", err)
			continue
		}
		srv := &http.Server{
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       time.Minute,
			IdleTimeout:       2 * time.Minute,
			BaseContext: func(net.Listener) context.Context {
				return context.WithValue(context.Background(), listenerKey{}, info)
			},
		}
		servers[addr] = srv
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				g.cfg.Log.Warn("telemetry gateway: serve", "addr", addr, "err", err)
			}
		}()
		g.cfg.Log.Info("telemetry gateway: listening", "addr", addr)
	}
}

// syncFirewall keeps a firewall rule for each project listener in want and
// none for others. Failures are logged once per distinct error, since Run
// retries every few seconds.
func (g *Gateway) syncFirewall(ctx context.Context, want map[string]listenerInfo) {
	if g.cfg.Firewall == nil {
		return
	}
	var rules []firewall.Rule
	for _, info := range want {
		if info.bridge != "" {
			rules = append(rules, firewall.Rule{Iface: info.bridge, Port: g.cfg.Port})
		}
	}
	slices.SortFunc(rules, func(a, b firewall.Rule) int { return strings.Compare(a.Iface, b.Iface) })
	err := g.cfg.Firewall.Sync(ctx, rules)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	if msg != g.firewallErr {
		g.firewallErr = msg
		if err != nil {
			g.cfg.Log.Warn("telemetry gateway: firewall", "err", err)
		}
	}
}

// reload reads the directory again and forgets the cached masks, so new
// variable values are masked from then on.
func (g *Gateway) reload(ctx context.Context) error {
	g.loadMu.Lock()
	defer g.loadMu.Unlock()
	return g.reloadLocked(ctx)
}

func (g *Gateway) reloadLocked(ctx context.Context) error {
	d, err := loadDirectory(ctx, g.cfg.Store, g.cfg.Docker)
	if err != nil {
		return err
	}
	g.dir.Store(d)
	g.loaded = g.now()
	g.masksMu.Lock()
	clear(g.masks)
	g.masksMu.Unlock()
	return nil
}

// reloadForMiss reloads the directory for a source it does not know, unless
// it was reloaded within missEvery, and returns the current directory.
func (g *Gateway) reloadForMiss(ctx context.Context) (*directory, error) {
	g.loadMu.Lock()
	defer g.loadMu.Unlock()
	if g.dir.Load() == nil || g.now().Sub(g.loaded) >= missEvery {
		if err := g.reloadLocked(ctx); err != nil {
			return nil, err
		}
	}
	return g.dir.Load(), nil
}

func (g *Gateway) loadCaps(ctx context.Context) error {
	s, err := g.cfg.Settings(ctx)
	if err != nil {
		return err
	}
	overrides, err := g.cfg.Store.AllTelemetryCaps(ctx)
	if err != nil {
		return err
	}
	g.caps.Store(&capsTable{defaults: s.DefaultCaps, overrides: overrides})
	return nil
}

// Attribute says who sent r, for [otlp.Config.Attribute]. On a project
// listener the source address must belong to a container of that project;
// on loopback everything is shed's, except the proxy's edge spans.
func (g *Gateway) Attribute(r *http.Request) (otlp.Attribution, error) {
	ctx := r.Context()
	info, _ := ctx.Value(listenerKey{}).(listenerInfo)
	if info.loopback {
		_, err := g.mask(ctx, shedKey)
		return otlp.Attribution{Source: shedSource, Span: g.edgeSpan}, err
	}
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil || info.projectID == "" {
		return otlp.Attribution{}, otlp.ErrUnknownSource
	}
	addr := ap.Addr().Unmap()
	src, ok := g.lookup(g.dir.Load(), addr, info.projectID)
	if !ok {
		d, err := g.reloadForMiss(ctx)
		if err != nil {
			return otlp.Attribution{}, err
		}
		if src, ok = g.lookup(d, addr, info.projectID); !ok {
			return otlp.Attribution{}, otlp.ErrUnknownSource
		}
	}
	if _, err := g.mask(ctx, keyOf(src)); err != nil {
		return otlp.Attribution{}, err
	}
	return otlp.Attribution{Source: src}, nil
}

func (g *Gateway) lookup(d *directory, addr netip.Addr, projectID string) (otlp.Source, bool) {
	if d == nil {
		return otlp.Source{}, false
	}
	src, ok := d.byAddr[addr]
	return src, ok && src.ProjectID == projectID
}

// edgeHostAttrs are the span attributes that may carry the request's host,
// in the order they are tried: Caddy's tracer sets server.address (newer
// semantic conventions) or http.host (older ones).
var edgeHostAttrs = []string{"server.address", "http.host"}

// edgeSpan attributes a span on the loopback listener. shed's own spans
// keep shed; the rest are the proxy's edge spans, attributed by the host
// of the request through the domain table, and to shed for the dashboard
// and unrouted hosts.
func (g *Gateway) edgeSpan(resource, span otlp.Attrs) (otlp.Source, bool) {
	if _, ok := resource.Lookup("shed.service.id"); ok {
		return otlp.Source{}, false
	}
	edge := shedSource
	edge.Origin = "edge"
	host := edgeHost(span)
	if d := g.dir.Load(); d != nil && host != "" {
		if src, ok := d.byHost[host]; ok {
			src.Origin = "edge"
			return src, true
		}
	}
	return edge, true
}

// edgeHost returns the host of an edge span's request, lowercase and
// without a port, or "".
func edgeHost(span otlp.Attrs) string {
	for _, k := range edgeHostAttrs {
		if v, ok := span.Lookup(k); ok && v != "" {
			return normalizeHost(v)
		}
	}
	if v, ok := span.Lookup("url.full"); ok {
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			return normalizeHost(u.Host)
		}
	}
	return ""
}

func normalizeHost(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(h, "."))
}

// Mask returns the masking function for src's records, for
// [otlp.Config.Mask]. Attribute has usually built it already; if building
// it fails now, every string is replaced rather than risk a leak.
func (g *Gateway) Mask(src otlp.Source) func(string) string {
	m, err := g.mask(context.Background(), keyOf(src))
	if err != nil {
		g.cfg.Log.Warn("telemetry gateway: mask", "service", src.ServiceID, "err", err)
		return func(string) string { return "[masked]" }
	}
	return m
}

// mask returns the cached masking function of k, building it if needed.
func (g *Gateway) mask(ctx context.Context, k maskKey) (func(string) string, error) {
	g.masksMu.Lock()
	m, ok := g.masks[k]
	g.masksMu.Unlock()
	if ok {
		return m, nil
	}
	var err error
	if k == shedKey {
		m, err = g.cfg.Masks.ShedMask(ctx)
	} else {
		m, err = g.cfg.Masks.TelemetryMask(ctx, k.projectID, k.deploymentID)
	}
	if err != nil {
		return nil, fmt.Errorf("observability: masks: %w", err)
	}
	g.masksMu.Lock()
	g.masks[k] = m
	g.masksMu.Unlock()
	return m, nil
}

// Caps returns a service's caps, for [otlp.Config.Caps]: its override, or
// the defaults. Before the first load there are none.
func (g *Gateway) Caps(serviceID string) otlp.Caps {
	t := g.caps.Load()
	if t == nil {
		return otlp.Caps{}
	}
	c := t.defaults
	if o, ok := t.overrides[serviceID]; ok {
		c = Caps{SpansPerSecond: o.SpansPerSecond, LogsPerSecond: o.LogsPerSecond, Series: o.Series}
	}
	return otlp.Caps{SpansPerSecond: c.SpansPerSecond, LogRecordsPerSecond: c.LogsPerSecond, Series: c.Series}
}
