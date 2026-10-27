// Package observability runs the telemetry stack: VictoriaMetrics,
// VictoriaLogs, VictoriaTraces, and Grafana, each a container on the host
// network listening on loopback.
//
// A [Manager] reconciles the containers with the operator's [Settings]: at
// boot and after every change it creates, recreates, starts, or stops them,
// provisions Grafana, and runs the registered workers (ingest, shippers)
// only while the stack is up.
package observability

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// Store keeps the settings. *store.Store implements it.
type Store interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	AddSetting(ctx context.Context, key, value string) (bool, error)
}

var _ Store = (*store.Store)(nil)

// Docker runs the components' containers. *docker.Client implements it.
type Docker interface {
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	ResolveImage(ctx context.Context, ref string) (string, error)
	PullImage(ctx context.Context, ref string, w io.Writer) error
	EnsureVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
	Run(ctx context.Context, spec docker.RunSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Restart(ctx context.Context, id string, timeout time.Duration) error
	Remove(ctx context.Context, id string) error
	Stats(ctx context.Context, id string) (docker.Stats, error)
	Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error
}

var _ Docker = (*docker.Client)(nil)

// Victoria is a client of one store. *victoria.Metrics, *victoria.Logs, and
// *victoria.Traces implement it.
type Victoria interface {
	Health(ctx context.Context) error
	DataSize(ctx context.Context) (int64, error)
}

// VictoriaMetrics also answers MetricsQL queries. *victoria.Metrics
// implements it.
type VictoriaMetrics interface {
	Victoria
	Query(ctx context.Context, q string, t time.Time) (*victoria.Result, error)
}

var (
	_ VictoriaMetrics = (*victoria.Metrics)(nil)
	_ Victoria        = (*victoria.Logs)(nil)
	_ Victoria        = (*victoria.Traces)(nil)
)

// GrafanaAPI is Grafana's HTTP API, as its admin. *grafana.Client
// implements it.
type GrafanaAPI interface {
	Health(ctx context.Context) (grafana.Health, error)
	EnsureFolder(ctx context.Context, f grafana.Folder) error
	ReloadAlertRules(ctx context.Context) error
	FiringAlerts(ctx context.Context) ([]grafana.Alert, error)
}

var _ GrafanaAPI = (*grafana.Client)(nil)

// Config configures a Manager.
type Config struct {
	Store   Store
	Docker  Docker
	Metrics VictoriaMetrics
	Logs    Victoria
	Traces  Victoria
	// Grafana returns a client that signs in with the admin password.
	Grafana func(password string) GrafanaAPI
	Addrs   Addrs
	// Dir is the telemetry directory, <data>/telemetry.
	Dir string
	// MinFreeBytes is the free disk space below which VictoriaMetrics stops
	// accepting writes.
	MinFreeBytes int64
	// GrafanaURL is the public URL Grafana is served at,
	// "<server.url>/grafana/".
	GrafanaURL string
	// OTLPEndpoint is the gateway address services send to.
	OTLPEndpoint string
	// Databases lists the database services to run exporters for. Nil runs
	// none.
	Databases ServiceStore
	// Redeployer redeploys databases that were started without query
	// statistics. Nil leaves them as they are.
	Redeployer Redeployer
	Log        *slog.Logger
}

// A Worker runs while telemetry is enabled and returns once ctx is done,
// which happens when telemetry is disabled or shed shuts down.
type Worker func(ctx context.Context)

// Manager runs the telemetry stack. It is safe for concurrent use.
type Manager struct {
	cfg  Config
	kick chan struct{}
	// poll is how often readiness is checked while waiting for a component.
	poll time.Duration

	workers []Worker
	hooks   []func(enabled bool)

	// base is the parent of the workers' context; Run cancels it.
	base     context.Context
	stopBase context.CancelFunc

	mu     sync.Mutex  // serializes Reconcile and DeleteData
	active *activation // non-nil while the workers run; guarded by mu

	// scrapeMu serializes writing scrape.yaml, and guards targets, the
	// exporters it names.
	scrapeMu sync.Mutex
	targets  []scrapeTarget
	dbs      *databases // nil without Config.Databases

	stateMu sync.Mutex
	errs    map[Component]string    // last reconcile failure; guarded by stateMu
	samples map[string]docker.Stats // last stats by container ID; guarded by stateMu
}

// activation is one run of the workers.
type activation struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New returns a Manager. It does nothing until Run.
func New(cfg Config) *Manager {
	base, stop := context.WithCancel(context.Background())
	m := &Manager{
		cfg:      cfg,
		kick:     make(chan struct{}, 1),
		poll:     time.Second,
		base:     base,
		stopBase: stop,
		errs:     make(map[Component]string),
		samples:  make(map[string]docker.Stats),
	}
	if cfg.Databases != nil {
		m.dbs = newDatabases(cfg, m.setTargets)
		m.workers = append(m.workers, m.dbs.watch, m.dbs.upgrade)
	}
	return m
}

// AddWorker registers w to run while telemetry is enabled: started once the
// stack is up, its context canceled when telemetry is disabled or shed
// shuts down. Register workers before Run.
func (m *Manager) AddWorker(w Worker) {
	m.workers = append(m.workers, w)
}

// OnEnabled registers fn, called with true once the stack is up and the
// workers have started, and with false when telemetry is disabled, before
// the workers stop. It is not called at shutdown. Register it before Run.
func (m *Manager) OnEnabled(fn func(enabled bool)) {
	m.hooks = append(m.hooks, fn)
}

// retryEvery is how long Run waits after a failed reconciliation before
// trying again.
const retryEvery = time.Minute

// Run reconciles the stack now, after every settings change, and a minute
// after a failure, until ctx is done. It then stops the workers and leaves
// the containers as they are.
func (m *Manager) Run(ctx context.Context) {
	defer m.shutdown()
	for {
		var retry <-chan time.Time
		if err := m.Reconcile(ctx); err != nil && ctx.Err() == nil {
			m.cfg.Log.Warn("reconcile telemetry", "err", err)
			retry = time.After(retryEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		case <-retry:
		}
	}
}

// shutdown stops the workers.
func (m *Manager) shutdown() {
	m.stopBase()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		m.active.wg.Wait()
		m.active = nil
	}
}

// activate starts the workers and calls the hooks, unless they run already.
func (m *Manager) activate() {
	if m.active != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.base)
	a := &activation{cancel: cancel}
	for _, w := range m.workers {
		a.wg.Go(func() { w(ctx) })
	}
	m.active = a
	for _, fn := range m.hooks {
		fn(true)
	}
}

// deactivate calls the hooks and stops the workers, if they run.
func (m *Manager) deactivate() {
	if m.active == nil {
		return
	}
	for _, fn := range m.hooks {
		fn(false)
	}
	m.active.cancel()
	m.active.wg.Wait()
	m.active = nil
}

// setErr records the outcome of reconciling c.
func (m *Manager) setErr(c Component, err error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if err == nil {
		delete(m.errs, c)
	} else {
		m.errs[c] = err.Error()
	}
}

// randomPassword returns a new Grafana admin password.
func randomPassword() string { return rand.Text() }
