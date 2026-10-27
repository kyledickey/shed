// Command shed runs the shed deployment platform: the dashboard, the API, the
// deploy pipeline, and the reverse proxy, in one process.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/kyledickey/shed/internal/api"
	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/config"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/firewall"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/host"
	"github.com/kyledickey/shed/internal/logtail"
	"github.com/kyledickey/shed/internal/mcp"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/s3"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry"
	"github.com/kyledickey/shed/internal/update"
	"github.com/kyledickey/shed/internal/variables"
	"github.com/kyledickey/shed/internal/victoria"
	"github.com/kyledickey/shed/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	// releaseRepo is the GitHub repository shed's releases are published to.
	releaseRepo = "kyledickey/shed"
	// releaseKey is the minisign public key that signs release checksums.
	releaseKey = "RWRBeRUdMkDg7PI7SQWikYL/5Evv0feGGQ07ZbVIfV5Q80snhMX7QaMK"
)

func main() {
	err := run()
	var re restartError
	if errors.As(err, &re) {
		// Every deferred cleanup in run has finished, so the new binary
		// finds the listeners and the database free. Go opens files with
		// close-on-exec, so nothing leaks across.
		err = syscall.Exec(re.binary, os.Args, os.Environ())
		err = fmt.Errorf("restart %s: %w", re.binary, err)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "shed:", err)
		os.Exit(1)
	}
}

// restartError asks main to replace the process with binary.
type restartError struct{ binary string }

func (e restartError) Error() string { return "restart into " + e.binary }

func run() error {
	configPath := flag.String("config", config.DefaultPath, "path to the configuration file")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	hostname, _ := os.Hostname()
	providers, err := telemetry.New(context.Background(), telemetry.Config{
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d", cfg.Telemetry.OTLPPort),
		Version:  version,
		Host:     hostname,
	})
	if err != nil {
		return err
	}
	providers.Register()
	tail := logtail.New(shedLogTail)
	log, err := newLogger(cfg.Log, config.LogFile(*configPath), tail, providers)
	if err != nil {
		return err
	}
	defer func() {
		if err := providers.Shutdown(context.Background()); err != nil {
			log.Warn("shut down telemetry providers", "err", err)
		}
	}()
	log.Info("starting shed", "version", version, "config", *configPath)
	binary, err := executable()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := cfg.Data.Dir
	logDir, buildDir := filepath.Join(dataDir, "logs"), filepath.Join(dataDir, "builds")
	backupDir := filepath.Join(dataDir, "backups")
	for _, dir := range []string{dataDir, logDir, buildDir, backupDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	key, keyPath, err := loadKey(config.KeyFile(*configPath), os.Getenv("CREDENTIALS_DIRECTORY"), log)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dataDir, "shed.db"), key)
	if errors.Is(err, store.ErrWrongKey) {
		return fmt.Errorf("shed.db was encrypted with a different key than %s; "+
			"restore the key it was encrypted with", keyPath)
	}
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.DeleteExpiredSessions(ctx); err != nil {
		log.Warn("delete expired sessions", "err", err)
	}

	dc, err := docker.New()
	if err != nil {
		return err
	}
	defer dc.Close()
	if err := dc.Ping(ctx); err != nil {
		return err
	}

	vm := victoria.NewMetrics("http://"+cfg.Telemetry.MetricsAddr, nil)
	vl := victoria.NewLogs("http://"+cfg.Telemetry.LogsAddr, nil)
	vt := victoria.NewTraces("http://"+cfg.Telemetry.TracesAddr, nil)

	var plane *control.Plane // Masks access records once telemetry runs.
	edge := observability.NewEdge(observability.EdgeConfig{
		Store:         st,
		Logs:          vl,
		Mask:          func(ctx context.Context) (func(string) string, error) { return plane.ShedMask(ctx) },
		DashboardHost: dashboardRoute(cfg.Server).Host,
		Log:           log,
	})
	var px *proxy.Proxy
	var routes deploy.Proxy // nil disables routing
	if cfg.Proxy.Enabled {
		px = proxy.New(proxy.Config{
			HTTPPort:   cfg.Proxy.HTTPPort,
			HTTPSPort:  cfg.Proxy.HTTPSPort,
			Email:      cfg.Proxy.ACMEEmail,
			StorageDir: filepath.Join(dataDir, "caddy"),
			LogFile:    filepath.Join(filepath.Dir(*configPath), "caddy.log"),
			Cloudflare: cfg.Proxy.Cloudflare,
			Access:     edge.Handle,
			TracesURL:  fmt.Sprintf("http://127.0.0.1:%d/v1/traces", cfg.Telemetry.OTLPPort),
		})
		defer func() {
			if err := px.Stop(); err != nil {
				log.Error("stop proxy", "err", err)
			}
		}()
		routes = edgeRoutes{px, edge}
	}

	vars := variables.New(st, onepassword.New(version, log))
	if err := vars.Preload(ctx); err != nil {
		log.Warn("load 1Password", "err", err)
	}

	otlpEndpoint := fmt.Sprintf("http://shed.internal:%d", cfg.Telemetry.OTLPPort)
	grafanaURL := strings.TrimRight(cfg.Server.URL, "/") + "/grafana/"
	newGrafana := func(password string) *grafana.Client {
		return grafana.New("http://"+cfg.Telemetry.GrafanaAddr+"/grafana", password, &http.Client{Timeout: 30 * time.Second})
	}
	stack := observability.New(observability.Config{
		Store:   st,
		Docker:  dc,
		Metrics: vm,
		Logs:    vl,
		Traces:  vt,
		Grafana: func(password string) observability.GrafanaAPI { return newGrafana(password) },
		Addrs: observability.Addrs{
			Metrics: cfg.Telemetry.MetricsAddr,
			Logs:    cfg.Telemetry.LogsAddr,
			Traces:  cfg.Telemetry.TracesAddr,
			Grafana: cfg.Telemetry.GrafanaAddr,
		},
		Dir:          filepath.Join(dataDir, "telemetry"),
		MinFreeBytes: int64(cfg.Build.MinFreeMB) << 20,
		GrafanaURL:   grafanaURL,
		OTLPEndpoint: otlpEndpoint,
		Log:          log,
	})

	dashboards := observability.NewDashboards(observability.DashboardsConfig{
		Store: st,
		Grafana: func(ctx context.Context) (observability.DashboardAPI, error) {
			pw, err := stack.AdminPassword(ctx)
			if err != nil {
				return nil, err
			}
			return newGrafana(pw), nil
		},
		GrafanaURL: grafanaURL,
		Log:        log,
	})
	stack.AddWorker(dashboards.Run)

	gh := &api.GitHubHolder{}
	deployer := deploy.New(deploy.Config{
		Store:  st,
		Docker: dc,
		Builder: &build.Builder{
			WorkDir:  buildDir,
			Instance: "shed",
			Memory:   int64(cfg.Build.MemoryMB) << 20,
			CPUs:     cfg.Build.CPUs,
			MinFree:  uint64(cfg.Build.MinFreeMB) << 20,
		},
		Proxy: routes,
		GitHub: func() (deploy.GitHub, bool) {
			if c := gh.Get(); c != nil {
				return c, true
			}
			return nil, false
		},
		Variables:       vars,
		LogDir:          logDir,
		LogLimit:        int64(cfg.Deployments.LogMaxMB) << 20,
		KeepDeployments: cfg.Deployments.Keep,
		Dashboard:       dashboardRoute(cfg.Server),
		Telemetry: func(ctx context.Context) (bool, error) {
			s, err := stack.Settings(ctx)
			return s.Enabled, err
		},
		OTLPEndpoint: otlpEndpoint,
		Events:       dashboards.Annotate,
		Log:          log,
	})
	defer deployer.Stop()

	backups := backup.New(backup.Config{
		Store:    st,
		Docker:   dc,
		Services: backupServices{deployer},
		NewRemote: func(c backup.S3Config) (backup.Remote, error) {
			return s3.New(s3.Config(c))
		},
		Dir:    backupDir,
		Events: dashboards.Annotate,
		Log:    log,
	})

	collector := metrics.New(metrics.Config{
		Docker: dc,
		Host:   host.Reader{Path: cfg.Data.Dir},
		Store:  st,
		Log:    log,
	})

	updater, err := update.New(update.Config{
		Repo:      releaseRepo,
		Version:   version,
		PublicKey: releaseKey,
		Binary:    binary,
		Dir:       filepath.Join(dataDir, "updates"),
		Settings:  updateSettings{st},
		Log:       log,
	})
	if err != nil {
		return err
	}
	var restarting atomic.Bool

	auditLog := audit.New(st, log)
	authn := auth.New(api.AuthStore(st), auditLog, func() (auth.OAuth, bool) {
		if c := gh.Get(); c != nil {
			return c, true
		}
		return nil, false
	}, cfg.Server.URL, cfg.Auth.AllowedUsers, log)

	if err := authn.RevokeDisallowedSessions(ctx); err != nil {
		return err
	}

	stack.AddWorker(edge.Run)
	if px != nil {
		stack.OnEnabled(func(on bool) {
			if err := px.SetTelemetry(on); err != nil {
				log.Error("switch proxy telemetry", "on", on, "err", err)
			}
		})
	}

	plane = control.New(control.Config{
		Store:    st,
		Deployer: deployer,
		Backups:  backups,
		Metrics:  collector,
		Logs:     tail,
		Updates:  updater,
		GitHub: func() (control.GitHub, bool) {
			if c := gh.Get(); c != nil {
				return c, true
			}
			return nil, false
		},
		Variables:  vars,
		Audit:      auditLog,
		Telemetry:  stack,
		Dashboards: dashboards,
		BaseURL:    cfg.Server.URL,
		BaseDomain: cfg.Proxy.BaseDomain,
		Log:        log,
	})

	gateway := observability.NewGateway(observability.GatewayConfig{
		Store:    st,
		Docker:   dc,
		Masks:    plane,
		Settings: stack.Settings,
		Port:     cfg.Telemetry.OTLPPort,
		Firewall: firewall.New(),
		Log:      log,
	})
	receiver, err := otlp.New(otlp.Config{
		Attribute: gateway.Attribute,
		Mask:      gateway.Mask,
		Caps:      gateway.Caps,
		Forwarder: observability.NewForwarder(vm, vt, vl),
		Logger:    log,
	})
	if err != nil {
		return err
	}
	stack.AddWorker(func(ctx context.Context) { gateway.Run(ctx, receiver) })
	stack.AddWorker(observability.NewLogShipper(observability.LogShipperConfig{
		Store: st, Docker: dc, Logs: vl, Masks: plane, Cap: receiver, Log: log,
	}).Run)
	stack.AddWorker(observability.NewAuditMirror(st, vl, log).Run)
	// shed's own telemetry flows through the loopback listener, which the
	// gateway opens once the stack is up.
	stack.OnEnabled(providers.SetEnabled)

	server, err := api.New(ctx, api.Config{
		Control:  plane,
		Settings: st,
		Restart: func() {
			restarting.Store(true)
			stop()
		},
		Auth:        authn,
		GitHub:      gh,
		BaseURL:     cfg.Server.URL,
		Web:         web.Dist(),
		MCP:         mcp.New(mcp.Config{Backend: plane, Version: version, Log: log}).Handler(),
		GrafanaAddr: cfg.Telemetry.GrafanaAddr,
		Log:         log,
	})
	if err != nil {
		return err
	}

	// Recover before Reconcile starts services: it puts back the data of
	// restores that a restart interrupted, and keeps the services whose data
	// it cannot put back stopped. Backups hold services through the
	// deployer, so Run waits until after Reconcile.
	if err := backups.Recover(ctx); err != nil {
		return err
	}
	if err := deployer.Reconcile(ctx); err != nil {
		return err
	}
	var background sync.WaitGroup
	background.Go(func() { collector.Run(ctx) })
	background.Go(func() { auditLog.Run(ctx) })
	background.Go(func() { backups.Run(ctx) })
	background.Go(func() { plane.ReplayPushes(ctx) })
	background.Go(func() { plane.SyncRoutes(ctx) })
	background.Go(func() { updater.Run(ctx) })
	background.Go(func() { stack.Run(ctx) })
	defer func() {
		stop() // Also ends the collector and backups when serve fails.
		background.Wait()
	}()
	if err := serve(ctx, cfg.Server.Listen, server.Handler(), log); err != nil {
		return err
	}
	if restarting.Load() {
		log.Info("restarting", "binary", binary)
		return restartError{binary}
	}
	return nil
}

// executable returns the path of the running shed binary, with symlinks
// resolved so an update replaces the file itself.
func executable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate shed binary: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("locate shed binary: %w", err)
	}
	return path, nil
}

// updateSettings adapts the store to update.Settings.
type updateSettings struct{ st *store.Store }

func (s updateSettings) Setting(ctx context.Context, key string) (string, bool, error) {
	v, err := s.st.Setting(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s updateSettings) SetSetting(ctx context.Context, key, value string) error {
	return s.st.SetSetting(ctx, key, value)
}

// edgeRoutes tells the edge to read the domain table again whenever routes
// are applied.
type edgeRoutes struct {
	*proxy.Proxy
	edge *observability.Edge
}

func (r edgeRoutes) Apply(routes []proxy.Route) error {
	defer r.edge.Refresh()
	return r.Proxy.Apply(routes)
}

// backupServices adapts the deployer to backup.Services.
type backupServices struct{ d *deploy.Deployer }

func (b backupServices) Hold(ctx context.Context, serviceID string) (backup.Held, error) {
	h, err := b.d.Hold(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return h, nil
}

// shedLogTail is how many lines of shed's log the dashboard can replay.
const shedLogTail = 1000

// newLogger returns a text logger writing to stderr, to a rotated file, and
// to tail, whose records also go to shed's telemetry while it is on.
func newLogger(cfg config.Log, file string, tail *logtail.Tail, providers *telemetry.Providers) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		return nil, fmt.Errorf("log level: %w", err)
	}
	rotated := &lumberjack.Logger{
		Filename:   file,
		MaxSize:    cfg.MaxSizeMB,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAgeDays,
	}
	// Stderr first: MultiWriter stops at the first failing writer.
	w := io.MultiWriter(os.Stderr, tail, rotated)
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(telemetry.NewSlogHandler(h, providers.LoggerProvider())), nil
}

// dashboardRoute routes the host of the dashboard URL to the listener.
func dashboardRoute(cfg config.Server) proxy.Route {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return proxy.Route{}
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return proxy.Route{}
	}
	if ip := net.ParseIP(host); host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return proxy.Route{Host: u.Hostname(), Upstream: net.JoinHostPort(host, port)}
}

// serve runs the HTTP server until ctx is done, then shuts it down. Open
// event streams are ended at shutdown rather than waited for.
func serve(ctx context.Context, addr string, h http.Handler, log *slog.Logger) error {
	base, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		BaseContext:       func(net.Listener) context.Context { return base },
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	srv.RegisterOnShutdown(cancelBase)

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", addr)

	select {
	case err := <-errc:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
