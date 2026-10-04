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
	"syscall"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/kyledickey/shed/internal/api"
	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/config"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shed:", err)
		os.Exit(1)
	}
}

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
	log, err := newLogger(cfg.Log, config.LogFile(*configPath))
	if err != nil {
		return err
	}
	log.Info("starting shed", "version", version, "config", *configPath)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir := cfg.Data.Dir
	logDir, buildDir := filepath.Join(dataDir, "logs"), filepath.Join(dataDir, "builds")
	for _, dir := range []string{dataDir, logDir, buildDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create data directory: %w", err)
		}
	}

	st, err := store.Open(filepath.Join(dataDir, "shed.db"))
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

	var routes deploy.Proxy // nil disables routing
	if cfg.Proxy.Enabled {
		px := proxy.New(proxy.Config{
			HTTPPort:   cfg.Proxy.HTTPPort,
			HTTPSPort:  cfg.Proxy.HTTPSPort,
			Email:      cfg.Proxy.ACMEEmail,
			StorageDir: filepath.Join(dataDir, "caddy"),
			LogFile:    filepath.Join(filepath.Dir(*configPath), "caddy.log"),
		})
		defer func() {
			if err := px.Stop(); err != nil {
				log.Error("stop proxy", "err", err)
			}
		}()
		routes = px
	}

	gh := &api.GitHubHolder{}
	deployer := deploy.New(deploy.Config{
		Store:   st,
		Docker:  dc,
		Builder: &build.Builder{WorkDir: buildDir},
		Proxy:   routes,
		GitHub: func() (deploy.GitHub, bool) {
			if c := gh.Get(); c != nil {
				return c, true
			}
			return nil, false
		},
		LogDir:    logDir,
		Dashboard: dashboardRoute(cfg.Server),
		Log:       log,
	})
	defer deployer.Stop()

	collector := metrics.New(metrics.Config{Docker: dc, Store: st, Log: log})

	authn := auth.New(api.AuthStore(st), func() (auth.OAuth, bool) {
		if c := gh.Get(); c != nil {
			return c, true
		}
		return nil, false
	}, cfg.Server.URL, cfg.Auth.AllowedUsers, log)

	if err := authn.RevokeDisallowedSessions(ctx); err != nil {
		return err
	}

	server, err := api.New(ctx, api.Config{
		Store:      st,
		Deployer:   deployer,
		Metrics:    collector,
		Auth:       authn,
		GitHub:     gh,
		BaseURL:    cfg.Server.URL,
		BaseDomain: cfg.Proxy.BaseDomain,
		Web:        web.Dist(),
		Log:        log,
	})
	if err != nil {
		return err
	}

	if err := deployer.Reconcile(ctx); err != nil {
		return err
	}
	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		collector.Run(ctx)
	}()
	defer func() {
		stop() // Also ends the collector when serve fails.
		<-collectorDone
	}()
	return serve(ctx, cfg.Server.Listen, server.Handler(), log)
}

// newLogger returns a text logger writing to stderr and to a rotated file.
func newLogger(cfg config.Log, file string) (*slog.Logger, error) {
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
	w := io.MultiWriter(os.Stderr, rotated)
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})), nil
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
