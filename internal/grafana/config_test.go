package grafana

import (
	"strings"
	"testing"
)

func envMap(t *testing.T, env []string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed %q", kv)
		}
		if _, dup := m[k]; dup {
			t.Fatalf("duplicate %s", k)
		}
		m[k] = v
	}
	return m
}

func TestEnv(t *testing.T) {
	env, err := Env(Config{
		RootURL:       "https://shed.example.com/grafana/",
		Addr:          "127.0.0.1:3001",
		AdminPassword: "x",
		Plugins:       []string{MetricsPlugin, LogsPlugin + "@0.20.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := envMap(t, env)
	want := map[string]string{
		"GF_PATHS_PROVISIONING":                 "/etc/grafana/provisioning",
		"GF_SERVER_HTTP_ADDR":                   "127.0.0.1",
		"GF_SERVER_HTTP_PORT":                   "3001",
		"GF_SERVER_ROOT_URL":                    "https://shed.example.com/grafana/",
		"GF_SERVER_SERVE_FROM_SUB_PATH":         "true",
		"GF_AUTH_PROXY_ENABLED":                 "true",
		"GF_AUTH_PROXY_HEADER_NAME":             "X-WEBAUTH-USER",
		"GF_AUTH_PROXY_HEADER_PROPERTY":         "username",
		"GF_AUTH_PROXY_HEADERS":                 "Name:X-WEBAUTH-NAME",
		"GF_AUTH_PROXY_AUTO_SIGN_UP":            "true",
		"GF_AUTH_PROXY_WHITELIST":               "127.0.0.1",
		"GF_USERS_ALLOW_SIGN_UP":                "false",
		"GF_USERS_AUTO_ASSIGN_ORG":              "true",
		"GF_USERS_AUTO_ASSIGN_ORG_ROLE":         "Admin",
		"GF_AUTH_DISABLE_LOGIN_FORM":            "true",
		"GF_AUTH_BASIC_ENABLED":                 "true",
		"GF_SECURITY_ADMIN_USER":                "shed",
		"GF_SECURITY_ADMIN_PASSWORD":            "x",
		"GF_SECURITY_DISABLE_GRAVATAR":          "true",
		"GF_SECURITY_ALLOW_EMBEDDING":           "false",
		"GF_ANALYTICS_REPORTING_ENABLED":        "false",
		"GF_ANALYTICS_CHECK_FOR_UPDATES":        "false",
		"GF_ANALYTICS_CHECK_FOR_PLUGIN_UPDATES": "false",
		"GF_PLUGINS_PREINSTALL_SYNC":            "victoriametrics-metrics-datasource,victoriametrics-logs-datasource@0.20.0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d variables, want %d: %v", len(got), len(want), got)
	}
}

func TestEnvRootPath(t *testing.T) {
	env, err := Env(Config{RootURL: "http://127.0.0.1:3001/", Addr: "127.0.0.1:3001", AdminPassword: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := envMap(t, env)["GF_SERVER_SERVE_FROM_SUB_PATH"]; got != "false" {
		t.Errorf("serve from sub path = %q, want false", got)
	}
}

func TestEnvInvalid(t *testing.T) {
	ok := Config{RootURL: "https://shed.example.com/grafana/", Addr: "127.0.0.1:3001", AdminPassword: "x"}
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"relative root URL", func(c *Config) { c.RootURL = "/grafana/" }},
		{"address without port", func(c *Config) { c.Addr = "127.0.0.1" }},
		{"no password", func(c *Config) { c.AdminPassword = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ok
			tt.change(&c)
			if _, err := Env(c); err == nil {
				t.Error("no error")
			}
		})
	}
}
