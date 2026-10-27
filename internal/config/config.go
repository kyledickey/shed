// Package config loads shed's configuration.
//
// Configuration is layered: built-in defaults, then an optional TOML file,
// then SHED_-prefixed environment variables. An environment variable name maps
// to a key by stripping the prefix, lowercasing, and replacing the first
// underscore with a dot, so SHED_PROXY_ACME_EMAIL sets proxy.acme_email.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// DefaultPath is the default location of the configuration file.
const DefaultPath = "/etc/shed/shed.toml"

// envPrefix is the prefix of environment variables read by [Load].
const envPrefix = "SHED_"

// Config is the complete shed configuration.
type Config struct {
	Server      Server      `koanf:"server"`
	Data        Data        `koanf:"data"`
	Proxy       Proxy       `koanf:"proxy"`
	Auth        Auth        `koanf:"auth"`
	Build       Build       `koanf:"build"`
	Deployments Deployments `koanf:"deployments"`
	Telemetry   Telemetry   `koanf:"telemetry"`
	Log         Log         `koanf:"log"`
}

// Telemetry configures where the telemetry components listen. Each address
// is a loopback host:port.
type Telemetry struct {
	MetricsAddr string `koanf:"metrics_addr"`
	LogsAddr    string `koanf:"logs_addr"`
	TracesAddr  string `koanf:"traces_addr"`
	GrafanaAddr string `koanf:"grafana_addr"`
	// OTLPPort is the port of the OTLP/HTTP gateway, on the host's loopback
	// and on loopback inside each app's container.
	OTLPPort int `koanf:"otlp_port"`
}

// Build limits the resources image builds may use.
type Build struct {
	// MemoryMB caps the memory of the BuildKit builder container, without
	// swap. Zero means unlimited.
	MemoryMB int `koanf:"memory_mb"`
	// CPUs caps the CPU time of the BuildKit builder container, in cores.
	// Zero means unlimited.
	CPUs float64 `koanf:"cpus"`
	// MinFreeMB is the free disk space the data directory's and Docker's
	// filesystems need for a build to start. Zero disables the check.
	MinFreeMB int `koanf:"min_free_mb"`
}

// Deployments bounds the deployment history kept on disk.
type Deployments struct {
	// LogMaxMB caps each deployment's build log; output beyond it is
	// dropped. Zero means unlimited.
	LogMaxMB int `koanf:"log_max_mb"`
	// Keep is how many deployments of each service are kept. Older finished
	// deployments and their logs are deleted. Zero keeps all of them.
	Keep int `koanf:"keep"`
}

// Server configures the dashboard and API listener.
type Server struct {
	// Listen is the address the dashboard and API listen on.
	Listen string `koanf:"listen"`
	// URL is the public dashboard URL, used for GitHub callbacks.
	URL string `koanf:"url"`
}

// Data configures where shed keeps its state.
type Data struct {
	// Dir holds the database, build directories, and deployment logs.
	Dir string `koanf:"dir"`
}

// Proxy configures the embedded reverse proxy.
type Proxy struct {
	Enabled   bool `koanf:"enabled"`
	HTTPPort  int  `koanf:"http_port"`
	HTTPSPort int  `koanf:"https_port"`
	// ACMEEmail is the contact address for certificate issuance.
	ACMEEmail string `koanf:"acme_email"`
	// BaseDomain is the parent domain of generated service domains.
	BaseDomain string `koanf:"base_domain"`
	// Cloudflare trusts Cloudflare's edge to report the client IP, for
	// domains proxied through Cloudflare.
	Cloudflare bool `koanf:"cloudflare"`
}

// Auth configures dashboard access.
type Auth struct {
	// AllowedUsers lists the GitHub logins that may access shed. An empty
	// list denies everyone. Changes take effect after restarting shed.
	AllowedUsers []string `koanf:"allowed_users"`
}

// Log configures application logging.
type Log struct {
	// Level is one of debug, info, warn, or error.
	Level      string `koanf:"level"`
	MaxSizeMB  int    `koanf:"max_size_mb"`
	MaxBackups int    `koanf:"max_backups"`
	MaxAgeDays int    `koanf:"max_age_days"`
}

// defaults returns the built-in configuration.
func defaults() Config {
	return Config{
		Server:      Server{Listen: "127.0.0.1:3000", URL: "http://localhost:3000"},
		Data:        Data{Dir: "/var/lib/shed"},
		Proxy:       Proxy{Enabled: true, HTTPPort: 80, HTTPSPort: 443},
		Build:       Build{MemoryMB: 2048, CPUs: 2, MinFreeMB: 2048},
		Deployments: Deployments{LogMaxMB: 10, Keep: 50},
		Telemetry: Telemetry{
			MetricsAddr: "127.0.0.1:8428",
			LogsAddr:    "127.0.0.1:9428",
			TracesAddr:  "127.0.0.1:10428",
			GrafanaAddr: "127.0.0.1:3001",
			OTLPPort:    4318,
		},
		Log: Log{Level: "info", MaxSizeMB: 20, MaxBackups: 5, MaxAgeDays: 30},
	}
}

// Load returns the configuration read from the TOML file at path and the
// environment, layered over the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	k := koanf.New(".")
	if _, err := os.Stat(path); err == nil {
		if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
			return Config{}, fmt.Errorf("config: load %s: %w", path, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Config{}, fmt.Errorf("config: stat %s: %w", path, err)
	}
	envProvider := env.Provider(".", env.Opt{Prefix: envPrefix, TransformFunc: transformEnv})
	if err := k.Load(envProvider, nil); err != nil {
		return Config{}, fmt.Errorf("config: load environment: %w", err)
	}

	cfg := defaults()
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("config: decode: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// transformEnv maps an environment variable to a configuration key and value.
func transformEnv(name, value string) (string, any) {
	key := strings.ToLower(strings.TrimPrefix(name, envPrefix))
	key = strings.Replace(key, "_", ".", 1)
	if key == "auth.allowed_users" {
		return key, splitList(value)
	}
	return key, value
}

// splitList splits a comma-separated list, trimming blanks.
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// LogFile returns the path of the application log for the configuration file
// at configPath: shed.log in the same directory.
func LogFile(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "shed.log")
}

// KeyFile returns the path of the key that encrypts secrets in the database,
// for the configuration file at configPath: shed.key in the same directory.
// It is kept apart from the data directory, so that copies of the data do
// not carry the key.
func KeyFile(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "shed.key")
}

// validate reports the first invalid setting in c.
func (c Config) validate() error {
	if c.Server.Listen == "" {
		return errors.New("config: server.listen must not be empty")
	}
	u, err := url.Parse(c.Server.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("config: server.url %q must be an http(s) URL with a host", c.Server.URL)
	}
	if c.Data.Dir == "" {
		return errors.New("config: data.dir must not be empty")
	}
	for name, port := range map[string]int{"proxy.http_port": c.Proxy.HTTPPort, "proxy.https_port": c.Proxy.HTTPSPort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("config: %s %d out of range 1-65535", name, port)
		}
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: log.level %q must be debug, info, warn, or error", c.Log.Level)
	}
	if c.Log.MaxSizeMB < 0 || c.Log.MaxBackups < 0 || c.Log.MaxAgeDays < 0 {
		return errors.New("config: log retention settings must not be negative")
	}
	if c.Build.MemoryMB < 0 || c.Build.CPUs < 0 || c.Build.MinFreeMB < 0 {
		return errors.New("config: build limits must not be negative")
	}
	if c.Deployments.LogMaxMB < 0 || c.Deployments.Keep < 0 {
		return errors.New("config: deployments limits must not be negative")
	}
	return c.Telemetry.validate()
}

// validate reports the first address that is not a loopback host:port, or a
// port out of range.
func (t Telemetry) validate() error {
	for _, a := range []struct{ key, addr string }{
		{"telemetry.metrics_addr", t.MetricsAddr},
		{"telemetry.logs_addr", t.LogsAddr},
		{"telemetry.traces_addr", t.TracesAddr},
		{"telemetry.grafana_addr", t.GrafanaAddr},
	} {
		host, port, err := net.SplitHostPort(a.addr)
		ip := net.ParseIP(host)
		n, perr := strconv.Atoi(port)
		if err != nil || ip == nil || !ip.IsLoopback() || perr != nil || n < 1 || n > 65535 {
			return fmt.Errorf("config: %s %q must be a loopback IP and port", a.key, a.addr)
		}
	}
	if t.OTLPPort < 1 || t.OTLPPort > 65535 {
		return fmt.Errorf("config: telemetry.otlp_port %d out of range 1-65535", t.OTLPPort)
	}
	return nil
}
