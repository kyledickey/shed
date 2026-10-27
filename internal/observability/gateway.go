package observability

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kyledickey/shed/internal/docker"
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

// GatewayDocker finds service containers and their processes.
// *docker.Client implements it.
type GatewayDocker interface {
	ContainerLister
	Inspect(ctx context.Context, id string) (docker.Container, error)
}

var _ GatewayDocker = (*docker.Client)(nil)

// Namespaces opens listeners inside the network namespaces of containers.
// cmd/shed adapts internal/netns to it.
type Namespaces interface {
	// ID identifies the network namespace of the process pid.
	ID(pid int) (uint64, error)
	// Listen listens on the TCP address addr inside the network namespace
	// of the process pid. An address the namespace does not have fails with
	// an error wrapping syscall.EADDRNOTAVAIL, a port taken there with one
	// wrapping syscall.EADDRINUSE.
	Listen(ctx context.Context, pid int, addr string) (net.Listener, error)
}

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
	Store      GatewayStore
	Docker     GatewayDocker
	Namespaces Namespaces
	Masks      Masks
	// Settings returns the telemetry settings, for the default caps.
	Settings func(ctx context.Context) (Settings, error)
	// Port is telemetry.otlp_port, the port of every listener.
	Port int
	Log  *slog.Logger
}

// Timings of the gateway.
const (
	// syncEvery is how often the listeners follow the containers, besides
	// whenever Kick is called, and the caps are reloaded.
	syncEvery = 5 * time.Second
	// masksEvery is how often the cached masking functions are rebuilt.
	masksEvery = 30 * time.Second
)

// Gateway receives OTLP for shed: it runs a listener on loopback inside
// every running app container and one on the host's loopback, and tells an
// [otlp.Handler] who sent each request, how to mask it, and what the
// sender's caps are.
type Gateway struct {
	cfg    GatewayConfig
	listen func(addr string) (net.Listener, error) // on the host
	now    func() time.Time
	kick   chan struct{}

	dir  atomic.Pointer[directory]
	caps atomic.Pointer[capsTable]
	// failed holds why listening failed, by service ID.
	failed atomic.Pointer[map[string]string]

	masksMu sync.Mutex
	masks   map[maskKey]func(string) string // cleared every masksEvery; guarded by masksMu
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
		kick:   make(chan struct{}, 1),
		masks:  make(map[maskKey]func(string) string),
	}
}

// listenerKey is the context key of the listener a request arrived on: an
// *appListener, or hostLoopback.
type listenerKey struct{}

// hostLoopback marks the listener on the host's loopback.
type hostLoopback struct{}

// appListener is the listener inside one app container's network
// namespace, identified by the process it was opened through and that
// namespace's ID.
type appListener struct {
	pid int
	ns  uint64
	// src is who the container's records belong to, kept current as the
	// service or project is renamed.
	src atomic.Pointer[otlp.Source]
	srv *http.Server // nil while listening fails
	err string       // why listening last failed
}

// Kick asks Run to follow the containers now, after one started or
// stopped. It does not block.
func (g *Gateway) Kick() {
	select {
	case g.kick <- struct{}{}:
	default:
	}
}

// ListenerError returns why the gateway cannot listen inside a running
// container of the service, such as the app having taken the port itself,
// or "".
func (g *Gateway) ListenerError(serviceID string) string {
	if m := g.failed.Load(); m != nil {
		return (*m)[serviceID]
	}
	return ""
}

// Run serves h on the listeners until ctx is done, following the app
// containers as they start, stop, and restart. It is meant to run as a
// [Worker].
func (g *Gateway) Run(ctx context.Context, h http.Handler) {
	var loopback *http.Server
	apps := make(map[string]*appListener) // by container ID
	defer func() {
		if loopback != nil {
			loopback.Close()
		}
		for _, l := range apps {
			l.close()
		}
		g.failed.Store(nil)
	}()
	tick := time.NewTicker(syncEvery)
	defer tick.Stop()
	masked := g.now()
	for {
		if loopback == nil {
			loopback = g.listenLoopback(h)
		}
		if err := g.loadCaps(ctx); err != nil {
			g.cfg.Log.Warn("telemetry gateway: load caps", "err", err)
		}
		if err := g.sync(ctx, apps, h); err != nil {
			g.cfg.Log.Warn("telemetry gateway: follow containers", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-g.kick:
		}
		if g.now().Sub(masked) >= masksEvery {
			g.forgetMasks()
			masked = g.now()
		}
	}
}

// listenLoopback starts serving h on the host's loopback, or returns nil if
// it cannot listen, to be retried on the next pass.
func (g *Gateway) listenLoopback(h http.Handler) *http.Server {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(g.cfg.Port))
	ln, err := g.listen(addr)
	if err != nil {
		g.cfg.Log.Warn("telemetry gateway: listen", "addr", addr, "err", err)
		return nil
	}
	srv := newServer(h, hostLoopback{})
	go g.serve(srv, ln)
	g.cfg.Log.Info("telemetry gateway: listening", "addr", addr)
	return srv
}

// sync opens a listener in every running app container that has none and
// closes those of containers that stopped, went away, or run in a new
// namespace since, which a restart gives them. Listening that failed is
// tried again. It also refreshes the domains edge spans are attributed by.
func (g *Gateway) sync(ctx context.Context, apps map[string]*appListener, h http.Handler) error {
	d, err := loadDirectory(ctx, g.cfg.Store, g.cfg.Docker)
	if err != nil {
		return err
	}
	g.dir.Store(d)
	running := make(map[string]otlp.Source)
	for _, c := range d.containers {
		if c.Running && c.Source.ServiceKind == "app" {
			running[c.ID] = c.Source
		}
	}
	for id, l := range apps {
		if _, ok := running[id]; ok && g.sameNamespace(l) {
			continue
		}
		l.close()
		delete(apps, id)
		g.cfg.Log.Info("telemetry gateway: closed listener", "container", id)
	}
	var errs []error
	for id, src := range running {
		l := apps[id]
		if l == nil {
			l, err = g.inspect(ctx, id)
			if err != nil {
				errs = append(errs, err)
			}
			if l == nil {
				continue
			}
			apps[id] = l
		}
		l.src.Store(&src)
		if l.srv == nil {
			g.open(ctx, id, l, h)
		}
	}
	failed := make(map[string]string)
	for _, l := range apps {
		if l.err != "" {
			failed[l.src.Load().ServiceID] = l.err
		}
	}
	g.failed.Store(&failed)
	return errors.Join(errs...)
}

// inspect returns a listener, not yet open, for the namespace of the
// container id, or nil if the container no longer runs.
func (g *Gateway) inspect(ctx context.Context, id string) (*appListener, error) {
	c, err := g.cfg.Docker.Inspect(ctx, id)
	if docker.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !c.Running || c.Pid == 0 {
		return nil, nil
	}
	ns, err := g.cfg.Namespaces.ID(c.Pid)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &appListener{pid: c.Pid, ns: ns}, nil
}

// sameNamespace reports whether l's process still runs in the namespace l
// was opened in. A restarted container has a new process and namespace.
func (g *Gateway) sameNamespace(l *appListener) bool {
	ns, err := g.cfg.Namespaces.ID(l.pid)
	return err == nil && ns == l.ns
}

// open starts serving h inside l's namespace on 127.0.0.1 and, if the
// namespace has IPv6 loopback, ::1, since localhost resolves to either. If
// either fails, neither serves and the error is kept in l, logged when it
// changes.
func (g *Gateway) open(ctx context.Context, id string, l *appListener, h http.Handler) {
	port := strconv.Itoa(g.cfg.Port)
	var lns []net.Listener
	var err error
	for _, host := range []string{"127.0.0.1", "::1"} {
		ln, lerr := g.cfg.Namespaces.Listen(ctx, l.pid, net.JoinHostPort(host, port))
		if host == "::1" && errors.Is(lerr, syscall.EADDRNOTAVAIL) {
			continue
		}
		if lerr != nil {
			err = lerr
			break
		}
		lns = append(lns, ln)
	}
	if err != nil {
		for _, ln := range lns {
			ln.Close()
		}
		msg := err.Error()
		if errors.Is(err, syscall.EADDRINUSE) {
			msg = fmt.Sprintf("The app listens on port %d itself, so shed cannot receive its OTLP at localhost:%d.", g.cfg.Port, g.cfg.Port)
		}
		if msg != l.err {
			g.cfg.Log.Warn("telemetry gateway: listen in container", "container", id, "service", l.src.Load().ServiceID, "err", err)
			l.err = msg
		}
		return
	}
	l.err = ""
	l.srv = newServer(h, l)
	for _, ln := range lns {
		go g.serve(l.srv, ln)
	}
	g.cfg.Log.Info("telemetry gateway: listening in container", "container", id, "service", l.src.Load().ServiceID, "listeners", len(lns))
}

func (l *appListener) close() {
	if l.srv != nil {
		l.srv.Close()
	}
}

// newServer returns a server of h whose requests carry listener under
// listenerKey.
func newServer(h http.Handler, listener any) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		IdleTimeout:       2 * time.Minute,
		BaseContext: func(net.Listener) context.Context {
			return context.WithValue(context.Background(), listenerKey{}, listener)
		},
	}
}

func (g *Gateway) serve(srv *http.Server, ln net.Listener) {
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		g.cfg.Log.Warn("telemetry gateway: serve", "addr", ln.Addr(), "err", err)
	}
}

// forgetMasks drops the cached masking functions, so changed variable
// values are masked from then on.
func (g *Gateway) forgetMasks() {
	g.masksMu.Lock()
	clear(g.masks)
	g.masksMu.Unlock()
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

// Attribute says who sent r, for [otlp.Config.Attribute]. A request on a
// container's listener is that container's service's; on the host's
// loopback everything is shed's, except the proxy's edge spans.
func (g *Gateway) Attribute(r *http.Request) (otlp.Attribution, error) {
	ctx := r.Context()
	switch l := ctx.Value(listenerKey{}).(type) {
	case *appListener:
		src := *l.src.Load()
		if _, err := g.mask(ctx, keyOf(src)); err != nil {
			return otlp.Attribution{}, err
		}
		return otlp.Attribution{Source: src}, nil
	case hostLoopback:
		_, err := g.mask(ctx, shedKey)
		return otlp.Attribution{Source: shedSource, Span: g.edgeSpan}, err
	}
	return otlp.Attribution{}, errors.New("observability: request on no gateway listener")
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
