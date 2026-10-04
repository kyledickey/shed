package catalog

import (
	"regexp"
	"slices"
	"testing"
)

func TestKinds(t *testing.T) {
	want := []string{"postgres", "mysql", "mongo", "redis"}
	if got := Kinds(); !slices.Equal(got, want) {
		t.Errorf("Kinds() = %v, want %v", got, want)
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		kind      string
		image     string
		port      int
		mountPath string
		cmd       bool
	}{
		{"postgres", "postgres:18-alpine", 5432, "/var/lib/postgresql", false},
		{"mysql", "mysql:9", 3306, "/var/lib/mysql", false},
		{"mongo", "mongo:8", 27017, "/data/db", false},
		{"redis", "redis:8-alpine", 6379, "/data", true},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got, ok := Lookup(tt.kind)
			if !ok {
				t.Fatal("Lookup() not found")
			}
			if got.Kind != tt.kind || got.Image != tt.image || got.Port != tt.port || got.MountPath != tt.mountPath {
				t.Errorf("Lookup() = %+v", got)
			}
			if (got.Cmd != nil) != tt.cmd {
				t.Errorf("Cmd = %v, want set: %v", got.Cmd, tt.cmd)
			}
		})
	}
	if _, ok := Lookup("nope"); ok {
		t.Error(`Lookup("nope") found a template`)
	}
}

func TestVars(t *testing.T) {
	secrets := map[string][]string{
		"postgres": {"POSTGRES_PASSWORD"},
		"mysql":    {"MYSQL_ROOT_PASSWORD"},
		"mongo":    {"MONGO_INITDB_ROOT_PASSWORD"},
		"redis":    {"REDIS_PASSWORD"},
	}
	urlKeys := map[string]string{
		"postgres": "DATABASE_URL",
		"mysql":    "DATABASE_URL",
		"mongo":    "MONGO_URL",
		"redis":    "REDIS_URL",
	}
	alnum24 := regexp.MustCompile(`^[A-Za-z0-9]{24}$`)

	for _, kind := range Kinds() {
		t.Run(kind, func(t *testing.T) {
			tmpl, _ := Lookup(kind)
			a, b := tmpl.Vars(), tmpl.Vars()
			for _, k := range secrets[kind] {
				if !alnum24.MatchString(a[k]) {
					t.Errorf("%s = %q, want 24 alphanumeric characters", k, a[k])
				}
				if a[k] == b[k] {
					t.Errorf("%s is not random across calls", k)
				}
			}
			if a[urlKeys[kind]] == "" {
				t.Errorf("missing %s", urlKeys[kind])
			}
		})
	}
}

func TestPostgresVars(t *testing.T) {
	tmpl, _ := Lookup("postgres")
	v := tmpl.Vars()
	want := "postgresql://${{POSTGRES_USER}}:${{POSTGRES_PASSWORD}}@${{SHED_PRIVATE_DOMAIN}}:5432/${{POSTGRES_DB}}"
	if v["DATABASE_URL"] != want {
		t.Errorf("DATABASE_URL = %q, want %q", v["DATABASE_URL"], want)
	}
	if v["POSTGRES_USER"] != "postgres" || v["POSTGRES_DB"] != "app" {
		t.Errorf("unexpected defaults: %v", v)
	}
}
