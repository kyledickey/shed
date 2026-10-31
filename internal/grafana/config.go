// Package grafana is everything shed knows about Grafana: the container's
// configuration, the provisioning files for data sources and default alert
// rules, a client for the HTTP API (folders, dashboards, annotations,
// alerts), and the dashboards shed creates, built in Go from templates.
package grafana

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// AdminUser is the Grafana admin that shed signs in as over basic auth.
const AdminUser = "shed"

// ProvisioningPath is where Grafana reads its provisioning files inside the
// container; mount the directory given to [WriteDatasources] and
// [WriteAlertRules] there.
const ProvisioningPath = "/etc/grafana/provisioning"

// The data source plugins Grafana needs for the stores.
const (
	MetricsPlugin = "victoriametrics-metrics-datasource"
	LogsPlugin    = "victoriametrics-logs-datasource"
)

// Config is what Grafana's container is started with.
type Config struct {
	// RootURL is the public URL Grafana is served at, such as
	// "https://shed.example.com/grafana/". Its path is the sub-path.
	RootURL string
	// Addr is the host:port Grafana listens on, such as "127.0.0.1:3001".
	Addr string
	// AdminPassword is the password of [AdminUser].
	AdminPassword string
	// Plugins are plugin IDs, optionally "id@version", installed before
	// Grafana starts.
	Plugins []string
}

// Env returns the container environment for c as KEY=VALUE pairs.
//
// Users sign in through shed: Grafana trusts X-WEBAUTH-USER from loopback
// only, signs up every user it has not seen as an organization Admin, and
// hides its login form. Basic auth stays on for shed's own API calls.
func Env(c Config) ([]string, error) {
	u, err := url.Parse(c.RootURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("grafana: root URL %q must be absolute", c.RootURL)
	}
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return nil, fmt.Errorf("grafana: listen address %q: %w", c.Addr, err)
	}
	if c.AdminPassword == "" {
		return nil, fmt.Errorf("grafana: admin password is empty")
	}
	subPath := strings.Trim(u.Path, "/") != ""
	vars := [][2]string{
		{"GF_PATHS_PROVISIONING", ProvisioningPath},
		{"GF_SERVER_HTTP_ADDR", host},
		{"GF_SERVER_HTTP_PORT", port},
		{"GF_SERVER_ROOT_URL", c.RootURL},
		{"GF_SERVER_SERVE_FROM_SUB_PATH", fmt.Sprint(subPath)},
		{"GF_AUTH_PROXY_ENABLED", "true"},
		{"GF_AUTH_PROXY_HEADER_NAME", "X-WEBAUTH-USER"},
		{"GF_AUTH_PROXY_HEADER_PROPERTY", "username"},
		{"GF_AUTH_PROXY_HEADERS", "Name:X-WEBAUTH-NAME"},
		{"GF_AUTH_PROXY_AUTO_SIGN_UP", "true"},
		{"GF_AUTH_PROXY_WHITELIST", "127.0.0.1"},
		{"GF_USERS_ALLOW_SIGN_UP", "false"},
		{"GF_USERS_AUTO_ASSIGN_ORG", "true"},
		{"GF_USERS_AUTO_ASSIGN_ORG_ROLE", "Admin"},
		{"GF_AUTH_DISABLE_LOGIN_FORM", "true"},
		{"GF_AUTH_BASIC_ENABLED", "true"},
		{"GF_SECURITY_ADMIN_USER", AdminUser},
		{"GF_SECURITY_ADMIN_PASSWORD", c.AdminPassword},
		{"GF_SECURITY_DISABLE_GRAVATAR", "true"},
		{"GF_SECURITY_ALLOW_EMBEDDING", "false"},
		{"GF_ANALYTICS_REPORTING_ENABLED", "false"},
		{"GF_ANALYTICS_CHECK_FOR_UPDATES", "false"},
		{"GF_ANALYTICS_CHECK_FOR_PLUGIN_UPDATES", "false"},
		// Synchronously, because the provisioned data sources need them.
		{"GF_PLUGINS_PREINSTALL_SYNC", strings.Join(c.Plugins, ",")},
	}
	env := make([]string, len(vars))
	for i, kv := range vars {
		env[i] = kv[0] + "=" + kv[1]
	}
	return env, nil
}
