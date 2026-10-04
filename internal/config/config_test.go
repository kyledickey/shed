package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shed.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := defaults(); !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
	if cfg.Server.URL != "http://localhost:3000" {
		t.Errorf("Server.URL = %q", cfg.Server.URL)
	}
}

func TestLoadFile(t *testing.T) {
	path := writeFile(t, `
[server]
url = "https://shed.example.com"

[proxy]
enabled = false
acme_email = "ops@example.com"

[auth]
allowed_users = ["alice", "bob"]

[log]
level = "debug"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.URL != "https://shed.example.com" || cfg.Server.Listen != "127.0.0.1:3000" {
		t.Errorf("Server = %+v", cfg.Server)
	}
	if cfg.Proxy.Enabled || cfg.Proxy.ACMEEmail != "ops@example.com" || cfg.Proxy.HTTPPort != 80 {
		t.Errorf("Proxy = %+v", cfg.Proxy)
	}
	if !reflect.DeepEqual(cfg.Auth.AllowedUsers, []string{"alice", "bob"}) {
		t.Errorf("AllowedUsers = %v", cfg.Auth.AllowedUsers)
	}
	if cfg.Log.Level != "debug" || cfg.Log.MaxSizeMB != 20 {
		t.Errorf("Log = %+v", cfg.Log)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	path := writeFile(t, "[server]\nurl = \"https://file.example.com\"\n")
	t.Setenv("SHED_SERVER_URL", "https://env.example.com")
	t.Setenv("SHED_PROXY_ACME_EMAIL", "env@example.com")
	t.Setenv("SHED_PROXY_HTTP_PORT", "8080")
	t.Setenv("SHED_PROXY_ENABLED", "false")
	t.Setenv("SHED_AUTH_ALLOWED_USERS", "alice, bob")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.URL != "https://env.example.com" {
		t.Errorf("Server.URL = %q", cfg.Server.URL)
	}
	if cfg.Proxy.ACMEEmail != "env@example.com" || cfg.Proxy.HTTPPort != 8080 || cfg.Proxy.Enabled {
		t.Errorf("Proxy = %+v", cfg.Proxy)
	}
	if !reflect.DeepEqual(cfg.Auth.AllowedUsers, []string{"alice", "bob"}) {
		t.Errorf("AllowedUsers = %v", cfg.Auth.AllowedUsers)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct{ name, toml, want string }{
		{"bad url", "[server]\nurl = \"shed.example.com\"\n", "server.url"},
		{"bad port", "[proxy]\nhttp_port = 70000\n", "proxy.http_port"},
		{"bad level", "[log]\nlevel = \"loud\"\n", "log.level"},
		{"bad toml", "[server\n", "load"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeFile(t, tt.toml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestLogFile(t *testing.T) {
	if got, want := LogFile("/etc/shed/shed.toml"), "/etc/shed/shed.log"; got != want {
		t.Errorf("LogFile() = %q, want %q", got, want)
	}
}
