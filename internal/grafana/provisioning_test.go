package grafana

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"go.yaml.in/yaml/v3"
)

func readYAML(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestWriteDatasources(t *testing.T) {
	dir := t.TempDir()
	stores := Stores{Metrics: "127.0.0.1:8428", Logs: "127.0.0.1:9428", Traces: "127.0.0.1:10428"}
	if err := WriteDatasources(dir, stores); err != nil {
		t.Fatal(err)
	}
	// Rewriting replaces the file.
	if err := WriteDatasources(dir, stores); err != nil {
		t.Fatal(err)
	}
	var f struct {
		APIVersion  int `yaml:"apiVersion"`
		Datasources []struct {
			Name     string         `yaml:"name"`
			Type     string         `yaml:"type"`
			UID      string         `yaml:"uid"`
			URL      string         `yaml:"url"`
			JSONData map[string]any `yaml:"jsonData"`
		} `yaml:"datasources"`
	}
	readYAML(t, filepath.Join(dir, "datasources", "shed.yaml"), &f)
	if f.APIVersion != 1 {
		t.Errorf("apiVersion = %d", f.APIVersion)
	}
	want := map[string][2]string{
		"shed-metrics": {"victoriametrics-metrics-datasource", "http://127.0.0.1:8428"},
		"shed-logs":    {"victoriametrics-logs-datasource", "http://127.0.0.1:9428"},
		"shed-traces":  {"jaeger", "http://127.0.0.1:10428/select/jaeger"},
	}
	if len(f.Datasources) != len(want) {
		t.Fatalf("got %d data sources, want %d", len(f.Datasources), len(want))
	}
	byUID := map[string]map[string]any{}
	for _, ds := range f.Datasources {
		w, ok := want[ds.UID]
		if !ok {
			t.Errorf("unexpected data source %q", ds.UID)
			continue
		}
		if ds.Type != w[0] || ds.URL != w[1] || ds.Name == "" {
			t.Errorf("%s: type %q url %q name %q, want %q %q", ds.UID, ds.Type, ds.URL, ds.Name, w[0], w[1])
		}
		byUID[ds.UID] = ds.JSONData
	}

	derived := byUID["shed-logs"]["derivedFields"].([]any)[0].(map[string]any)
	if derived["datasourceUid"] != "shed-traces" || derived["matcherRegex"] != "trace_id" {
		t.Errorf("logs derived field = %v, want trace_id linking to shed-traces", derived)
	}
	toLogs := byUID["shed-traces"]["tracesToLogsV2"].(map[string]any)
	if toLogs["datasourceUid"] != "shed-logs" || toLogs["query"] != `trace_id:="${__trace.traceId}"` {
		t.Errorf("traces to logs = %v, want a trace ID query of shed-logs", toLogs)
	}
}

func TestWriteAlertRules(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAlertRules(dir); err != nil {
		t.Fatal(err)
	}
	var f struct {
		APIVersion int `yaml:"apiVersion"`
		Groups     []struct {
			Name     string `yaml:"name"`
			Folder   string `yaml:"folder"`
			Interval string `yaml:"interval"`
			Rules    []struct {
				UID       string `yaml:"uid"`
				Title     string `yaml:"title"`
				Condition string `yaml:"condition"`
				For       string `yaml:"for"`
				Data      []struct {
					RefID         string         `yaml:"refId"`
					DatasourceUID string         `yaml:"datasourceUid"`
					Model         map[string]any `yaml:"model"`
				} `yaml:"data"`
			} `yaml:"rules"`
		} `yaml:"groups"`
	}
	readYAML(t, filepath.Join(dir, "alerting", "shed.yaml"), &f)
	if f.APIVersion != 1 || len(f.Groups) != 1 {
		t.Fatalf("apiVersion %d, %d groups", f.APIVersion, len(f.Groups))
	}
	g := f.Groups[0]
	if g.Name != "shed-defaults" || g.Folder != "shed" || g.Interval == "" {
		t.Errorf("group %q in folder %q every %q", g.Name, g.Folder, g.Interval)
	}
	wantUIDs := []string{
		"shed-restarts", "shed-oom", "shed-host-disk", "shed-host-memory", "shed-http-errors",
		"shed-http-latency", "shed-db-connections", "shed-deploy-failed", "shed-backup-failed", "shed-telemetry-drops",
	}
	if len(g.Rules) != len(wantUIDs) {
		t.Fatalf("got %d rules, want %d", len(g.Rules), len(wantUIDs))
	}
	uidRE := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	for i, r := range g.Rules {
		if r.UID != wantUIDs[i] {
			t.Errorf("rule %d uid = %q, want %q", i, r.UID, wantUIDs[i])
		}
		if !uidRE.MatchString(r.UID) || r.Title == "" || r.For == "" {
			t.Errorf("rule %q: invalid uid, title %q, or for %q", r.UID, r.Title, r.For)
		}
		refs := map[string]bool{}
		for _, d := range r.Data {
			refs[d.RefID] = true
		}
		if !refs[r.Condition] {
			t.Errorf("rule %q: condition %q is not a query", r.UID, r.Condition)
		}
		q := r.Data[0]
		if q.DatasourceUID != "shed-metrics" || q.Model["expr"] == "" || q.Model["instant"] != true {
			t.Errorf("rule %q: query %v on %q", r.UID, q.Model, q.DatasourceUID)
		}
		th := r.Data[1]
		if th.DatasourceUID != "__expr__" || th.Model["type"] != "threshold" || th.Model["expression"] != q.RefID {
			t.Errorf("rule %q: threshold %v", r.UID, th.Model)
		}
	}
}
