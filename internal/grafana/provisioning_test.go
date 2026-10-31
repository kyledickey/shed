package grafana

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

type alertRuleYAML struct {
	UID         string            `yaml:"uid"`
	Title       string            `yaml:"title"`
	Condition   string            `yaml:"condition"`
	For         string            `yaml:"for"`
	IsPaused    bool              `yaml:"isPaused"`
	Annotations map[string]string `yaml:"annotations"`
	Data        []struct {
		RefID         string         `yaml:"refId"`
		DatasourceUID string         `yaml:"datasourceUid"`
		Model         map[string]any `yaml:"model"`
	} `yaml:"data"`
}

type alertFileYAML struct {
	APIVersion int `yaml:"apiVersion"`
	Groups     []struct {
		Name     string          `yaml:"name"`
		Folder   string          `yaml:"folder"`
		Interval string          `yaml:"interval"`
		Rules    []alertRuleYAML `yaml:"rules"`
	} `yaml:"groups"`
	DeleteRules []struct {
		OrgID int    `yaml:"orgId"`
		UID   string `yaml:"uid"`
	} `yaml:"deleteRules"`
}

// threshold returns the value the rule's threshold query compares against.
func (r alertRuleYAML) threshold(t *testing.T) float64 {
	t.Helper()
	cond := r.Data[1].Model["conditions"].([]any)[0].(map[string]any)
	switch v := cond["evaluator"].(map[string]any)["params"].([]any)[0].(type) {
	case float64:
		return v
	case int: // YAML writes 2.0 as 2.
		return float64(v)
	default:
		t.Fatalf("threshold %v is a %T", v, v)
		return 0
	}
}

func TestWriteAlertRules(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAlertRules(dir, nil); err != nil {
		t.Fatal(err)
	}
	var f alertFileYAML
	readYAML(t, filepath.Join(dir, "alerting", "shed.yaml"), &f)
	if f.APIVersion != 1 || len(f.Groups) != 1 {
		t.Fatalf("apiVersion %d, %d groups", f.APIVersion, len(f.Groups))
	}
	g := f.Groups[0]
	if g.Name != "shed-defaults" || g.Folder != "shed" || g.Interval == "" {
		t.Errorf("group %q in folder %q every %q", g.Name, g.Folder, g.Interval)
	}
	want := []struct {
		uid       string
		threshold float64
		forText   string
		summary   string
	}{
		{"shed-host-disk", 0.85, "0s", "the host disk is more than 85% full"},
		{"shed-host-memory", 0.9, "600s", "host memory has been above 90% for 10 minutes"},
		{"shed-http-errors", 0.05, "300s", "more than 5% of requests to {{ $labels.shed_service_name }} have failed with 5xx for 5 minutes"},
		{"shed-http-latency", 2, "600s", "p95 latency of {{ $labels.shed_service_name }} has been above 2 s for 10 minutes"},
		{"shed-db-connections", 0.8, "300s", "{{ $labels.shed_service_name }} has used more than 80% of its maximum connections for 5 minutes"},
		{"shed-telemetry-drops", 0, "300s", "shed has been dropping telemetry from {{ $labels.shed_service_name }} for 5 minutes"},
	}
	if len(g.Rules) != len(want) {
		t.Fatalf("got %d rules, want %d", len(g.Rules), len(want))
	}
	uidRE := regexp.MustCompile(`^[a-zA-Z0-9_-]{1,40}$`)
	for i, r := range g.Rules {
		w := want[i]
		if r.UID != w.uid {
			t.Errorf("rule %d uid = %q, want %q", i, r.UID, w.uid)
		}
		if !uidRE.MatchString(r.UID) || r.Title == "" || r.IsPaused {
			t.Errorf("rule %q: invalid uid, title %q, or paused %v", r.UID, r.Title, r.IsPaused)
		}
		if r.For != w.forText || r.Annotations["summary"] != w.summary {
			t.Errorf("rule %q: for %q, summary %q, want %q, %q", r.UID, r.For, r.Annotations["summary"], w.forText, w.summary)
		}
		if got := r.threshold(t); got != w.threshold {
			t.Errorf("rule %q: threshold %v, want %v", r.UID, got, w.threshold)
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
	var deleted []string
	for _, d := range f.DeleteRules {
		if d.OrgID != 1 {
			t.Errorf("delete of %s in org %d", d.UID, d.OrgID)
		}
		deleted = append(deleted, d.UID)
	}
	if want := []string{"shed-restarts", "shed-oom", "shed-deploy-failed", "shed-backup-failed"}; !slices.Equal(deleted, want) {
		t.Errorf("deleteRules = %v, want %v", deleted, want)
	}
}

func TestWriteAlertRulesOverrides(t *testing.T) {
	th := func(v float64) *float64 { return &v }
	secs := func(v int) *int { return &v }
	dir := t.TempDir()
	err := WriteAlertRules(dir, map[string]AlertOverride{
		"shed-host-memory":     {Threshold: th(72.5), ForSeconds: secs(3600)},
		"shed-http-latency":    {Threshold: th(0.5), ForSeconds: secs(0)},
		"shed-http-errors":     {Paused: true},
		"shed-db-connections":  {ForSeconds: secs(90)},
		"shed-host-disk":       {ForSeconds: secs(60)}, // not editable
		"shed-telemetry-drops": {Threshold: th(5)},     // not editable
		"shed-unknown":         {Paused: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var f alertFileYAML
	readYAML(t, filepath.Join(dir, "alerting", "shed.yaml"), &f)
	got := map[string]alertRuleYAML{}
	for _, r := range f.Groups[0].Rules {
		got[r.UID] = r
	}
	for _, tt := range []struct {
		uid       string
		threshold float64
		forText   string
		paused    bool
		summary   string
	}{
		{uid: "shed-host-memory", threshold: 0.725, forText: "3600s", summary: "host memory has been above 72.5% for 1 hour"},
		{uid: "shed-http-latency", threshold: 0.5, forText: "0s", summary: "p95 latency of {{ $labels.shed_service_name }} has been above 0.5 s"},
		{uid: "shed-http-errors", threshold: 0.05, forText: "300s", paused: true, summary: "more than 5% of requests to {{ $labels.shed_service_name }} have failed with 5xx for 5 minutes"},
		{uid: "shed-db-connections", threshold: 0.8, forText: "90s", summary: "{{ $labels.shed_service_name }} has used more than 80% of its maximum connections for 90 seconds"},
		{uid: "shed-host-disk", threshold: 0.85, forText: "0s", summary: "the host disk is more than 85% full"},
		{uid: "shed-telemetry-drops", threshold: 0, forText: "300s", summary: "shed has been dropping telemetry from {{ $labels.shed_service_name }} for 5 minutes"},
	} {
		r := got[tt.uid]
		if v := r.threshold(t); v != tt.threshold || r.For != tt.forText || r.IsPaused != tt.paused || r.Annotations["summary"] != tt.summary {
			t.Errorf("%s: threshold %v, for %q, paused %v, summary %q; want %+v", tt.uid, v, r.For, r.IsPaused, r.Annotations["summary"], tt)
		}
	}
}

func TestWriteWebhook(t *testing.T) {
	dir := t.TempDir()
	if err := WriteWebhook(dir, "http://127.0.0.1:4318/hook", "tok"); err != nil {
		t.Fatal(err)
	}
	var f struct {
		APIVersion    int `yaml:"apiVersion"`
		ContactPoints []struct {
			OrgID     int    `yaml:"orgId"`
			Name      string `yaml:"name"`
			Receivers []struct {
				UID      string            `yaml:"uid"`
				Type     string            `yaml:"type"`
				Settings map[string]string `yaml:"settings"`
			} `yaml:"receivers"`
		} `yaml:"contactPoints"`
		Policies []struct {
			OrgID          int      `yaml:"orgId"`
			Receiver       string   `yaml:"receiver"`
			GroupBy        []string `yaml:"group_by"`
			GroupWait      string   `yaml:"group_wait"`
			GroupInterval  string   `yaml:"group_interval"`
			RepeatInterval string   `yaml:"repeat_interval"`
		} `yaml:"policies"`
	}
	readYAML(t, filepath.Join(dir, "alerting", "notify.yaml"), &f)
	if f.APIVersion != 1 || len(f.ContactPoints) != 1 || len(f.Policies) != 1 {
		t.Fatalf("file = %+v", f)
	}
	cp := f.ContactPoints[0]
	if cp.OrgID != 1 || cp.Name != "shed" || len(cp.Receivers) != 1 {
		t.Fatalf("contact point = %+v", cp)
	}
	r := cp.Receivers[0]
	if r.UID == "" || r.Type != "webhook" || r.Settings["url"] != "http://127.0.0.1:4318/hook" ||
		r.Settings["authorization_scheme"] != "Bearer" || r.Settings["authorization_credentials"] != "tok" {
		t.Errorf("receiver = %+v", r)
	}
	p := f.Policies[0]
	if p.OrgID != 1 || p.Receiver != "shed" || !slices.Equal(p.GroupBy, []string{"alertname", "shed_service_id"}) ||
		p.GroupWait != "30s" || p.GroupInterval != "5m" || p.RepeatInterval != "4h" {
		t.Errorf("policy = %+v", p)
	}
}
