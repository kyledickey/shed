package proxy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func decode(t *testing.T, b []byte) obj {
	t.Helper()
	var v obj
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b)
	}
	return v
}

// at walks nested JSON values by object key or array index.
func at(t *testing.T, v any, path ...any) any {
	t.Helper()
	for _, p := range path {
		switch p := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("%v: not an object at %q", v, p)
			}
			v = m[p]
		case int:
			s, ok := v.([]any)
			if !ok || p >= len(s) {
				t.Fatalf("%v: no index %d", v, p)
			}
			v = s[p]
		}
	}
	return v
}

var baseCfg = Config{HTTPPort: 80, HTTPSPort: 443}

func TestBuildConfigRoutes(t *testing.T) {
	routes := []Route{
		{Host: "b.example.com", Upstream: "10.0.0.2:3000"},
		{Host: "A.example.com ", Upstream: "10.0.0.1:3000"},
	}
	got, err := buildConfig(baseCfg, routes)
	if err != nil {
		t.Fatal(err)
	}
	conf := decode(t, got)

	if d := at(t, conf, "admin", "disabled"); d != true {
		t.Errorf("admin.disabled = %v, want true", d)
	}
	srv := at(t, conf, "apps", "http", "servers", "shed")
	if l := at(t, srv, "listen", 0); l != ":443" {
		t.Errorf("listen = %v, want :443", l)
	}
	if p := at(t, conf, "apps", "http", "http_port"); p != float64(80) {
		t.Errorf("http_port = %v", p)
	}

	var hosts, dials []any
	for i := 0; i < 2; i++ {
		hosts = append(hosts, at(t, srv, "routes", i, "match", 0, "host", 0))
		dials = append(dials, at(t, srv, "routes", i, "handle", 0, "upstreams", 0, "dial"))
	}
	if want := []any{"a.example.com", "b.example.com"}; !reflect.DeepEqual(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}
	if want := []any{"10.0.0.1:3000", "10.0.0.2:3000"}; !reflect.DeepEqual(dials, want) {
		t.Errorf("dials = %v, want %v", dials, want)
	}
	if h := at(t, srv, "routes", 0, "handle", 0, "handler"); h != "reverse_proxy" {
		t.Errorf("handler = %v", h)
	}
	// Exactly two host routes plus the catch-all.
	if n := len(at(t, srv, "routes").([]any)); n != 3 {
		t.Errorf("len(routes) = %d, want 3", n)
	}
	if h := at(t, srv, "routes", 2, "handle", 0, "handler"); h != "static_response" {
		t.Errorf("last handler = %v, want static_response", h)
	}
}

func TestBuildConfigDeterministic(t *testing.T) {
	a := []Route{{"a.example.com", "1:1"}, {"b.example.com", "2:2"}, {"c.example.com", "3:3"}}
	b := []Route{a[2], a[0], a[1]}
	ja, err := buildConfig(baseCfg, a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := buildConfig(baseCfg, b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ja) != string(jb) {
		t.Errorf("config depends on route order:\n%s\n%s", ja, jb)
	}
}

func TestBuildConfigDoesNotMutateRoutes(t *testing.T) {
	routes := []Route{{"B.example.com", "1:1"}, {"a.example.com", "2:2"}}
	if _, err := buildConfig(baseCfg, routes); err != nil {
		t.Fatal(err)
	}
	if routes[0].Host != "B.example.com" || routes[1].Host != "a.example.com" {
		t.Errorf("routes mutated: %v", routes)
	}
}

func TestBuildConfigErrors(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		routes []Route
	}{
		{"no ports", Config{}, nil},
		{"no https port", Config{HTTPPort: 80}, nil},
		{"empty host", baseCfg, []Route{{"", "1:1"}}},
		{"empty upstream", baseCfg, []Route{{"a.example.com", ""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildConfig(tt.cfg, tt.routes); err == nil {
				t.Error("buildConfig() succeeded, want error")
			}
		})
	}
}

func TestBuildConfigOptions(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		got, _ := buildConfig(baseCfg, []Route{{"app.example.com", "1:1"}})
		conf := decode(t, got)
		if _, ok := conf["storage"]; ok {
			t.Error("storage set without StorageDir")
		}
		if _, ok := at(t, conf, "apps").(obj)["tls"]; ok {
			t.Error("tls app set for a public host without an email")
		}
		if o := at(t, conf, "logging", "logs", "default", "writer", "output"); o != "discard" {
			t.Errorf("log output = %v, want discard", o)
		}
		if v := at(t, conf, "apps", "pki", "certificate_authorities", "local", "install_trust"); v != false {
			t.Errorf("install_trust = %v, want false", v)
		}
	})
	t.Run("storage and log file", func(t *testing.T) {
		cfg := baseCfg
		cfg.StorageDir, cfg.LogFile = "/var/lib/shed/certs", "/var/lib/shed/caddy.log"
		got, _ := buildConfig(cfg, nil)
		conf := decode(t, got)
		if r := at(t, conf, "storage", "root"); r != "/var/lib/shed/certs" {
			t.Errorf("storage root = %v", r)
		}
		if f := at(t, conf, "logging", "logs", "default", "writer", "filename"); f != "/var/lib/shed/caddy.log" {
			t.Errorf("log filename = %v", f)
		}
	})
	t.Run("custom ports", func(t *testing.T) {
		got, _ := buildConfig(Config{HTTPPort: 8080, HTTPSPort: 8443}, nil)
		conf := decode(t, got)
		if l := at(t, conf, "apps", "http", "servers", "shed", "listen", 0); l != ":8443" {
			t.Errorf("listen = %v", l)
		}
		if p := at(t, conf, "apps", "http", "https_port"); p != float64(8443) {
			t.Errorf("https_port = %v", p)
		}
	})
}

func TestBuildConfigTLSPolicies(t *testing.T) {
	routes := []Route{
		{"app.example.com", "1:1"},
		{"localhost", "2:2"},
		{"203.0.113.7", "3:3"},
		{"dev.localhost", "4:4"},
	}
	tests := []struct {
		name         string
		email        string
		wantPolicies []obj
	}{
		{
			name:  "email",
			email: "ops@example.com",
			wantPolicies: []obj{
				{"subjects": []any{"app.example.com"}, "issuers": []any{obj{"module": "acme", "email": "ops@example.com"}}},
				{"subjects": []any{"203.0.113.7", "dev.localhost", "localhost"}, "issuers": []any{obj{"module": "internal"}}},
			},
		},
		{
			name: "no email",
			wantPolicies: []obj{
				{"subjects": []any{"203.0.113.7", "dev.localhost", "localhost"}, "issuers": []any{obj{"module": "internal"}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.Email = tt.email
			got, err := buildConfig(cfg, routes)
			if err != nil {
				t.Fatal(err)
			}
			policies := at(t, decode(t, got), "apps", "tls", "automation", "policies").([]any)
			if len(policies) != len(tt.wantPolicies) {
				t.Fatalf("got %d policies, want %d: %v", len(policies), len(tt.wantPolicies), policies)
			}
			for i, want := range tt.wantPolicies {
				if !reflect.DeepEqual(policies[i], any(map[string]any(want))) {
					t.Errorf("policy %d = %v, want %v", i, policies[i], want)
				}
			}
		})
	}
}

func TestDuplicateRoutesRejected(t *testing.T) {
	for _, host := range []string{"dashboard.example.com", "DASHBOARD.example.com.", " dashboard.example.com "} {
		_, err := buildConfig(baseCfg, []Route{{Host: "dashboard.example.com", Upstream: "127.0.0.1:3000"}, {Host: host, Upstream: "10.0.0.2:8080"}})
		if err == nil {
			t.Errorf("duplicate %q accepted", host)
		}
	}
}
