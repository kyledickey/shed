//go:build integration

// This test needs Docker and network access, and that no shed-telemetry-*
// containers or volumes exist. It runs the real telemetry stack through the
// Manager next to one real database of each kind, started from the catalog
// templates with their start arguments, and reads what VictoriaMetrics
// scraped from the exporters. Concurrent runs collide on the fixed container
// names, so run it under a lock:
//
//	flock /tmp/shed-telemetry-integration.lock go test -tags integration -run DatabaseExporters -v -timeout 25m ./internal/observability/
package observability_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

func TestIntegrationDatabaseExporters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 22*time.Minute)
	defer cancel()
	for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
		name := "shed-telemetry-" + c
		if out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^"+name+"$").Output(); len(out) > 0 {
			t.Fatalf("container %s exists; this test would replace it", name)
		}
		if exec.Command("docker", "volume", "inspect", name).Run() == nil {
			t.Fatalf("volume %s exists; this test would delete it", name)
		}
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dc, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	defer dc.Close()

	project, err := st.CreateProject(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	network := "shed-" + project.ID

	// cleanup runs even when the test fails or is interrupted by a timeout.
	var leftovers []string
	t.Cleanup(func() {
		if t.Failed() {
			for _, c := range []string{"shed-telemetry-metrics"} {
				logs, _ := exec.Command("docker", "logs", "--tail", "40", c).CombinedOutput()
				t.Logf("%s logs:\n%s", c, logs)
			}
		}
		for _, c := range append(leftovers, "shed-telemetry-grafana", "shed-telemetry-traces", "shed-telemetry-logs", "shed-telemetry-metrics") {
			exec.Command("docker", "rm", "-f", "-v", c).Run()
		}
		for _, c := range []string{"metrics", "logs", "traces", "grafana"} {
			exec.Command("docker", "volume", "rm", "-f", "shed-telemetry-"+c).Run()
		}
		exec.Command("docker", "network", "rm", network).Run()
	})

	if err := dc.EnsureNetwork(ctx, network); err != nil {
		t.Fatal(err)
	}
	type db struct {
		svc  store.Service
		dep  store.Deployment
		ctr  string
		ip   string
		vars map[string]string
	}
	var dbs []*db
	for _, kind := range catalog.Kinds() {
		tmpl, _ := catalog.Lookup(kind)
		svc, err := st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: kind + "-main", Kind: kind, Image: tmpl.Image, Port: tmpl.Port})
		if err != nil {
			t.Fatal(err)
		}
		leftovers = append(leftovers, "shed-exp-"+svc.ID, "it-db-"+svc.ID)
		if err := dc.PullImage(ctx, tmpl.Image, os.Stderr); err != nil {
			t.Fatal(err)
		}
		vars := tmpl.Vars()
		var env []string
		for k, v := range vars {
			env = append(env, k+"="+v)
		}
		id, err := dc.Run(ctx, docker.RunSpec{
			Name: "it-db-" + svc.ID, Image: tmpl.Image, Cmd: tmpl.Cmd, Env: env, Network: network, Aliases: []string{svc.Name},
		})
		if err != nil {
			t.Fatal(err)
		}
		c, err := dc.Inspect(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		dbs = append(dbs, &db{svc: svc, ctr: id, ip: c.IPs[network], vars: vars})
	}
	for _, d := range dbs {
		addr := net.JoinHostPort(d.ip, strconv.Itoa(d.svc.Port))
		deadline := time.Now().Add(3 * time.Minute)
		for {
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				conn.Close()
				break
			}
			if time.Now().After(deadline) {
				logs, _ := dc.Inspect(ctx, d.ctr)
				t.Fatalf("%s did not accept connections at %s: %v (%s)", d.svc.Name, addr, err, logs.State)
			}
			time.Sleep(time.Second)
		}
		tmpl, _ := catalog.Lookup(d.svc.Kind)
		dep, err := st.CreateDeployment(ctx, store.Deployment{ServiceID: d.svc.ID, Status: store.StatusDeploying, Trigger: store.TriggerCreate})
		if err != nil {
			t.Fatal(err)
		}
		dep.ContainerID, dep.Port, dep.Image = d.ctr, d.svc.Port, tmpl.Image
		dep.Runtime = &store.DeploymentRuntime{Cmd: tmpl.Cmd, Env: d.vars}
		if err := st.ActivateDeployment(ctx, dep); err != nil {
			t.Fatal(err)
		}
		d.dep = dep
	}

	freeAddr := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().String()
	}
	addrs := observability.Addrs{Metrics: freeAddr(), Logs: freeAddr(), Traces: freeAddr(), Grafana: freeAddr()}
	metrics := victoria.NewMetrics("http://"+addrs.Metrics, nil)
	stack := observability.New(observability.Config{
		Store:   st,
		Docker:  dc,
		Metrics: metrics,
		Logs:    victoria.NewLogs("http://"+addrs.Logs, nil),
		Traces:  victoria.NewTraces("http://"+addrs.Traces, nil),
		Grafana: func(pw string) observability.GrafanaAPI {
			return grafana.New("http://"+addrs.Grafana+"/grafana", pw, &http.Client{Timeout: 30 * time.Second})
		},
		Addrs:        addrs,
		Dir:          filepath.Join(t.TempDir(), "telemetry"),
		GrafanaURL:   "http://" + addrs.Grafana + "/grafana/",
		OTLPEndpoint: "http://shed.internal:4318",
		Databases:    st,
		Log:          log,
	})
	t.Cleanup(func() {
		off := observability.DefaultSettings()
		stack.SetSettings(context.Background(), off)
		stack.Reconcile(context.Background())
	})
	set := func(on bool) {
		t.Helper()
		s := observability.DefaultSettings()
		s.Enabled = on
		if _, err := stack.SetSettings(ctx, s); err != nil {
			t.Fatal(err)
		}
		if err := stack.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	set(true)

	// mysqld-exporter reports only statements run in a schema.
	for _, d := range dbs {
		if d.svc.Kind != "mysql" {
			continue
		}
		var out bytes.Buffer
		if err := dc.Exec(ctx, d.ctr, []string{"sh", "-c", `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -D "$MYSQL_DATABASE" -e 'select 1'`}, nil, &out, &out); err != nil {
			t.Fatalf("query in %s: %v: %s", d.svc.Name, err, out.String())
		}
	}

	// query waits until the instant query q has a series with every label
	// in want and a value accepted by ok.
	query := func(q string, want map[string]string, ok func(float64) bool) {
		t.Helper()
		deadline := time.Now().Add(4 * time.Minute)
		var last string
		for {
			res, err := metrics.Query(ctx, q, time.Time{})
			last = fmt.Sprintf("%+v, %v", res, err)
			if err == nil {
				for _, s := range res.Series {
					match := len(s.Values) > 0 && ok(s.Values[0].Value)
					for k, v := range want {
						match = match && s.Labels[k] == v
					}
					if match {
						return
					}
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s never matched %v: %s", q, want, last)
			}
			time.Sleep(3 * time.Second)
		}
	}
	nonzero := func(v float64) bool { return v > 0 }
	one := func(v float64) bool { return v == 1 }
	present := func(float64) bool { return true }

	checks := map[string][]struct {
		metric string
		ok     func(float64) bool
	}{
		"postgres": {{"pg_up", one}, {"pg_stat_database_xact_commit", nonzero}, {"pg_stat_statements_calls_total", nonzero}, {"pg_stat_statements_query_id", present}},
		"mysql":    {{"mysql_up", one}, {"mysql_global_status_queries", nonzero}, {"mysql_perf_schema_events_statements_seconds_total", present}},
		"mongo":    {{"mongodb_up", one}, {"mongodb_ss_opcounters", present}},
		"redis":    {{"redis_up", one}, {"redis_commands_processed_total", nonzero}},
	}
	for _, d := range dbs {
		labels := map[string]string{
			"shed_service_id":   d.svc.ID,
			"shed_service_name": d.svc.Name,
			"shed_service_kind": d.svc.Kind,
			"shed_project_id":   project.ID,
			"shed_source":       "exporter",
		}
		query(fmt.Sprintf(`up{shed_service_id=%q}`, d.svc.ID), labels, one)
		for _, c := range checks[d.svc.Kind] {
			query(fmt.Sprintf(`%s{shed_service_id=%q}`, c.metric, d.svc.ID), map[string]string{"shed_service_id": d.svc.ID, "shed_service_kind": d.svc.Kind}, c.ok)
		}
	}

	// The slow query log reaches the container's output.
	for _, d := range dbs {
		var script string
		switch d.svc.Kind {
		case "mysql":
			script = `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -e 'select sleep(0.5)'`
		case "postgres":
			script = `PGPASSWORD="$POSTGRES_PASSWORD" psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c 'select pg_sleep(0.5)'`
		default:
			continue
		}
		var out bytes.Buffer
		if err := dc.Exec(ctx, d.ctr, []string{"sh", "-c", script}, nil, &out, &out); err != nil {
			t.Fatalf("slow query in %s: %v: %s", d.svc.Name, err, out.String())
		}
		want := map[string]string{"mysql": "Query_time", "postgres": "duration: "}[d.svc.Kind]
		deadline := time.Now().Add(30 * time.Second)
		for {
			var logs bytes.Buffer
			if err := dc.Logs(ctx, d.ctr, 200, false, &logs); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(logs.String(), want) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s logs lack %q:\n%s", d.svc.Name, want, logs.String())
			}
			time.Sleep(time.Second)
		}
	}

	set(false)
	for _, d := range dbs {
		out, _ := exec.Command("docker", "ps", "-aq", "--filter", "name=^shed-exp-"+d.svc.ID+"$").Output()
		if len(out) > 0 {
			t.Errorf("exporter of %s exists after telemetry was turned off", d.svc.Name)
		}
	}
}
