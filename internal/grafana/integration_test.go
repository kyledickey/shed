//go:build integration

// This test needs Docker and network access. It starts Grafana and the
// three stores, provisions Grafana, pushes every dashboard, and runs every
// dashboard query through Grafana:
//
//	go test -tags integration -run Integration -v ./internal/grafana/
package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func image(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	out, err := exec.Command("docker", append([]string{"run", "-d", "--rm", "--name", name, "--network", "host"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run %s: %v\n%s", name, err, out)
	}
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command("docker", "logs", "--tail", "50", name).CombinedOutput()
			t.Logf("%s logs:\n%s", name, logs)
		}
		exec.Command("docker", "rm", "-f", name).Run()
	})
}

func TestIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	suffix := fmt.Sprint(time.Now().UnixNano())
	stores := Stores{Metrics: freeAddr(t), Logs: freeAddr(t), Traces: freeAddr(t)}
	run(t, "shed-it-metrics-"+suffix, image("VM_IMAGE", "victoriametrics/victoria-metrics:latest"), "-httpListenAddr="+stores.Metrics)
	run(t, "shed-it-logs-"+suffix, image("VL_IMAGE", "victoriametrics/victoria-logs:latest"), "-httpListenAddr="+stores.Logs)
	run(t, "shed-it-traces-"+suffix, image("VT_IMAGE", "victoriametrics/victoria-traces:latest"), "-httpListenAddr="+stores.Traces)

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteDatasources(dir, stores); err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	base := "http://" + addr + "/grafana"
	env, err := Env(Config{RootURL: base + "/", Addr: addr, AdminPassword: "it-password", Plugins: []string{MetricsPlugin, LogsPlugin}})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-v", dir + ":" + ProvisioningPath + ":ro"}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	run(t, "shed-it-grafana-"+suffix, append(args, image("GRAFANA_IMAGE", "grafana/grafana:latest"))...)

	c := New(base, "it-password", &http.Client{Timeout: 30 * time.Second})
	for {
		h, err := c.Health(ctx)
		if err == nil {
			t.Logf("Grafana %s", h.Version)
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Grafana did not become healthy: %v", err)
		case <-time.After(2 * time.Second):
		}
	}

	for _, uid := range []string{MetricsUID, LogsUID, TracesUID} {
		var ds struct {
			Type string `json:"type"`
		}
		if err := c.do(ctx, http.MethodGet, "/api/datasources/uid/"+uid, nil, &ds); err != nil {
			t.Fatalf("data source %s: %v", uid, err)
		}
	}

	t.Run("auth proxy", func(t *testing.T) {
		get := func(path string, v any) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
			req.Header.Set("X-WEBAUTH-USER", "octocat")
			req.Header.Set("X-WEBAUTH-NAME", "The Octocat")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: %s", path, resp.Status)
			}
			json.NewDecoder(resp.Body).Decode(v)
		}
		var user struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		}
		get("/api/user", &user)
		var orgs []struct {
			Role string `json:"role"`
		}
		get("/api/user/orgs", &orgs)
		if user.Login != "octocat" || user.Name != "The Octocat" || len(orgs) != 1 || orgs[0].Role != "Admin" {
			t.Errorf("signed up %+v with orgs %+v, want octocat as Admin", user, orgs)
		}
	})

	if err := c.EnsureFolder(ctx, Folder{UID: ShedFolder, Title: "shed"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteAlertRules(dir); err != nil {
		t.Fatal(err)
	}
	if err := c.ReloadAlertRules(ctx); err != nil {
		t.Fatal(err)
	}
	var rules []struct {
		UID       string `json:"uid"`
		FolderUID string `json:"folderUID"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules", nil, &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != len(defaultAlertRules()) {
		t.Errorf("%d alert rules provisioned, want %d", len(rules), len(defaultAlertRules()))
	}
	for _, r := range rules {
		if r.FolderUID != ShedFolder {
			t.Errorf("rule %s in folder %q, want %q", r.UID, r.FolderUID, ShedFolder)
		}
	}
	if _, err := c.FiringAlerts(ctx); err != nil {
		t.Error(err)
	}

	for _, f := range []Folder{
		{UID: ArchivedFolder, Title: "Archived"},
		{UID: ProjectFolder("proj00000000"), Title: "shop"},
	} {
		if err := c.EnsureFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	type placed struct {
		folder string
		d      Dashboard
	}
	var all []placed
	for _, d := range ShedDashboards() {
		all = append(all, placed{ShedFolder, d})
	}
	for _, d := range ProjectDashboards("proj00000000") {
		all = append(all, placed{ProjectFolder("proj00000000"), d})
	}
	for _, kind := range []string{"app", "postgres", "mysql", "mongo", "redis"} {
		id := (kind + "000000000000")[:12]
		f := Folder{UID: ServiceFolder(id), Title: kind, ParentUID: ProjectFolder("proj00000000")}
		if err := c.EnsureFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
		for _, d := range ServiceDashboards("proj00000000", id, kind) {
			all = append(all, placed{f.UID, d})
		}
	}
	for _, p := range all {
		v, err := c.PutDashboard(ctx, p.folder, p.d.JSON, 0)
		if err != nil {
			t.Fatalf("create %s: %v", p.d.UID, err)
		}
		got, err := c.Dashboard(ctx, p.d.UID)
		if err != nil || got.Version != v || got.FolderUID != p.folder {
			t.Errorf("%s: %+v, %v, want version %d in %s", p.d.UID, got, err, v, p.folder)
		}
		if _, err := c.PutDashboard(ctx, p.folder, p.d.JSON, 0); !errors.Is(err, ErrConflict) {
			t.Errorf("%s: creating again = %v, want ErrConflict", p.d.UID, err)
		}
		changed := strings.Replace(string(p.d.JSON), `"refresh":"1m"`, `"refresh":"5m"`, 1)
		v2, err := c.PutDashboard(ctx, p.folder, json.RawMessage(changed), v)
		if err != nil || v2 <= v {
			t.Errorf("%s: overwrite = %d, %v", p.d.UID, v2, err)
		}
		if _, err := c.PutDashboard(ctx, p.folder, p.d.JSON, v); !errors.Is(err, ErrConflict) {
			t.Errorf("%s: stale overwrite = %v, want ErrConflict", p.d.UID, err)
		}
	}
	hits, err := c.SearchDashboards(ctx, SearchQuery{FolderUIDs: []string{ServiceFolder("app000000000")}})
	if err != nil || len(hits) != 1 {
		t.Errorf("search app folder = %d hits, %v", len(hits), err)
	}

	archived := Folder{UID: ServiceFolder("app000000000"), Title: "shop / app (deleted 2026-10-26)", ParentUID: ArchivedFolder}
	if err := c.EnsureFolder(ctx, archived); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Folder(ctx, archived.UID); err != nil || got != archived {
		t.Errorf("archived folder = %+v, %v", got, err)
	}

	if _, err := c.CreateAnnotation(ctx, Annotation{Time: time.Now(), Tags: []string{"shed", "deploy.active", "service:app000000000"}, Text: "deployed"}); err != nil {
		t.Error(err)
	}

	t.Run("queries", func(t *testing.T) {
		for _, p := range all {
			var m struct {
				Panels []struct {
					Title   string           `json:"title"`
					Targets []map[string]any `json:"targets"`
				} `json:"panels"`
			}
			json.Unmarshal(p.d.JSON, &m)
			for _, panel := range m.Panels {
				for _, q := range panel.Targets {
					checkQuery(ctx, t, c, p.d.UID+" / "+panel.Title, q)
				}
			}
		}
	})
}

// checkQuery runs q through Grafana with the dashboard variables set and
// fails on a query error. The stores are empty, so this checks syntax.
func checkQuery(ctx context.Context, t *testing.T, c *Client, where string, q map[string]any) {
	t.Helper()
	// Grafana's front end interpolates these before sending queries.
	vars := strings.NewReplacer("$__rate_interval", "1m", "$__range", "1h", "$host", "example.com", "$search", "*", "$service_name", "app")
	for _, k := range []string{"expr", "service"} {
		if s, ok := q[k].(string); ok {
			q[k] = vars.Replace(s)
		}
	}
	body := map[string]any{"queries": []any{q}, "from": "now-1h", "to": "now"}
	var resp struct {
		Results map[string]struct {
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/ds/query", body, &resp); err != nil {
		t.Errorf("%s: %v\n%v", where, err, q["expr"])
		return
	}
	for ref, r := range resp.Results {
		if r.Error != "" {
			t.Errorf("%s / %s: %s\n%v", where, ref, r.Error, q["expr"])
		}
	}
}
