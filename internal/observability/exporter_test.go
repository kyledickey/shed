package observability

import (
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/store"
)

// testDatabase returns a database of kind with the variables of its
// template, started the way the deployer starts it.
func testDatabase(kind string, withStatements bool) database {
	tmpl, _ := catalog.Lookup(kind)
	vars := tmpl.Vars()
	for k := range vars {
		if strings.HasSuffix(k, "PASSWORD") {
			vars[k] = "pw-" + kind
		}
	}
	rt := &store.DeploymentRuntime{Env: vars}
	if withStatements {
		rt.Cmd = tmpl.Cmd
	}
	return database{
		svc:     store.Service{ID: "svc" + kind, ProjectID: "prj1", Name: kind + "-main", Kind: kind, Port: tmpl.Port},
		project: store.Project{ID: "prj1", Name: "shop"},
		dep:     store.Deployment{ID: "dep-" + kind, ServiceID: "svc" + kind, ContainerID: "ctr-" + kind, Port: tmpl.Port, Runtime: rt},
	}
}

func TestExporterSpec(t *testing.T) {
	tests := []struct {
		kind string
		// args are the flags the exporter must run with.
		args []string
		// env holds the variables of the database whose values the
		// exporter takes as environment, by exporter variable.
		env map[string]string
		// secret is the variable of the database that must reach the
		// exporter only through its environment.
		secret string
	}{
		{
			kind: "postgres",
			args: []string{"--collector.stat_statements", "--collector.stat_statements.include_query"},
			env: map[string]string{
				"DATA_SOURCE_URI":  "postgres-main:5432/app?sslmode=disable",
				"DATA_SOURCE_USER": "postgres",
			},
			secret: "POSTGRES_PASSWORD",
		},
		{
			kind:   "mysql",
			args:   []string{"--mysqld.address=mysql-main:3306", "--mysqld.username=root", "--collect.perf_schema.eventsstatements", "--collect.info_schema.innodb_metrics"},
			secret: "MYSQL_ROOT_PASSWORD",
		},
		{
			kind:   "mongo",
			args:   []string{"--collect-all"},
			secret: "MONGO_INITDB_ROOT_PASSWORD",
		},
		{
			kind:   "redis",
			env:    map[string]string{"REDIS_ADDR": "redis-main:6379"},
			secret: "REDIS_PASSWORD",
		},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			db := testDatabase(tt.kind, true)
			s := exporterSpec(db)
			if !strings.Contains(s.Image, "@sha256:") || !strings.Contains(s.Image, ":") {
				t.Errorf("image %q is not pinned by tag and digest", s.Image)
			}
			if !slices.Equal(s.Cmd, tt.args) {
				t.Errorf("args = %v, want %v", s.Cmd, tt.args)
			}
			if s.Name != "shed-exp-svc"+tt.kind || s.Network != "shed-prj1" || len(s.Aliases) != 0 {
				t.Errorf("name %q on %q with aliases %v", s.Name, s.Network, s.Aliases)
			}
			if s.CPUs != 0.1 || s.Memory != 64<<20 {
				t.Errorf("limits = %v CPUs, %d bytes, want 0.1 and 64 MiB", s.CPUs, s.Memory)
			}
			if s.Labels[LabelExporter] != "svc"+tt.kind || s.Labels[labelExporterSpec] == "" {
				t.Errorf("labels = %v", s.Labels)
			}
			for k, want := range tt.env {
				if !slices.Contains(s.Env, k+"="+want) {
					t.Errorf("env lacks %s=%s: %v", k, want, s.Env)
				}
			}
			password := db.dep.Runtime.Env[tt.secret]
			if !strings.Contains(strings.Join(s.Env, "\n"), password) {
				t.Errorf("env %v lacks the password", s.Env)
			}
			for _, field := range slices.Concat(s.Cmd, []string{s.Name, s.Image}, mapValues(s.Labels)) {
				if strings.Contains(field, password) {
					t.Errorf("password appears outside the environment: %q", field)
				}
			}
		})
	}
}

func mapValues(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestExporterSpecHash(t *testing.T) {
	db := testDatabase("postgres", true)
	base := exporterSpec(db).Labels[labelExporterSpec]
	if again := exporterSpec(db).Labels[labelExporterSpec]; again != base {
		t.Error("hash changed for an unchanged database")
	}
	changed := func(name string, edit func(*database)) {
		t.Helper()
		d := testDatabase("postgres", true)
		edit(&d)
		if exporterSpec(d).Labels[labelExporterSpec] == base {
			t.Errorf("%s did not change the hash", name)
		}
	}
	changed("password", func(d *database) { d.dep.Runtime.Env["POSTGRES_PASSWORD"] = "other" })
	changed("renamed service", func(d *database) { d.svc.Name = "renamed" })
	changed("database", func(d *database) { d.dep.Runtime.Env["POSTGRES_DB"] = "other" })
	changed("started without statistics", func(d *database) { d.dep.Runtime.Cmd = nil })

	d := testDatabase("postgres", true)
	d.dep.ID = "dep-new"
	if exporterSpec(d).Labels[labelExporterSpec] != base {
		t.Error("a new deployment recreates the exporter")
	}
}

func TestPostgresStatementsOnlyWhenStarted(t *testing.T) {
	spec := exporterSpec(testDatabase("postgres", false))
	if len(spec.Cmd) != 0 {
		t.Errorf("a database started without pg_stat_statements got flags %v", spec.Cmd)
	}
}

func TestExporterEnvEscapes(t *testing.T) {
	db := testDatabase("mongo", true)
	db.dep.Runtime.Env["MONGO_INITDB_ROOT_PASSWORD"] = "p@ss/word:1"
	s := exporterSpec(db)
	want := "MONGODB_URI=mongodb://mongo:p%40ss%2Fword%3A1@mongo-main:27017/admin"
	if !slices.Contains(s.Env, want) {
		t.Errorf("env = %v, want %s", s.Env, want)
	}
}

func TestScrapeConfig(t *testing.T) {
	addrs := Addrs{Metrics: "127.0.0.1:8428", Logs: "127.0.0.1:9428", Traces: "127.0.0.1:10428"}
	pg, my := testDatabase("postgres", true), testDatabase("mysql", true)
	targets := []scrapeTarget{targetFor(pg, "172.20.0.5"), targetFor(my, "172.20.0.6")}

	b, err := yaml.Marshal(scrapeConfigOf(addrs, targets))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ScrapeConfigs []struct {
			JobName        string `yaml:"job_name"`
			ScrapeInterval string `yaml:"scrape_interval"`
			StaticConfigs  []struct {
				Targets []string          `yaml:"targets"`
				Labels  map[string]string `yaml:"labels"`
			} `yaml:"static_configs"`
		} `yaml:"scrape_configs"`
	}
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.ScrapeConfigs) != 2 || got.ScrapeConfigs[0].JobName != "shed-telemetry" {
		t.Fatalf("jobs = %+v", got.ScrapeConfigs)
	}
	job := got.ScrapeConfigs[1]
	if job.JobName != "shed-exporters" || job.ScrapeInterval != "15s" || len(job.StaticConfigs) != 2 {
		t.Fatalf("exporter job = %+v", job)
	}
	first := job.StaticConfigs[0]
	if !slices.Equal(first.Targets, []string{"172.20.0.5:9187"}) {
		t.Errorf("targets = %v", first.Targets)
	}
	wantLabels := map[string]string{
		"shed_project_id":    "prj1",
		"shed_project_name":  "shop",
		"shed_service_id":    "svcpostgres",
		"shed_service_name":  "postgres-main",
		"shed_service_kind":  "postgres",
		"shed_deployment_id": "dep-postgres",
		"shed_source":        "exporter",
	}
	for k, v := range wantLabels {
		if first.Labels[k] != v {
			t.Errorf("label %s = %q, want %q", k, first.Labels[k], v)
		}
	}
	if len(first.Labels) != len(wantLabels) {
		t.Errorf("labels = %v", first.Labels)
	}
	if job.StaticConfigs[1].Targets[0] != "172.20.0.6:9104" {
		t.Errorf("mysql target = %v", job.StaticConfigs[1].Targets)
	}

	none := scrapeConfigOf(addrs, nil)
	if len(none.ScrapeConfigs) != 1 {
		t.Errorf("without exporters the config has %d jobs, want only the stores", len(none.ScrapeConfigs))
	}
}
