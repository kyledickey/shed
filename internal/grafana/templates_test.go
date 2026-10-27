package grafana

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// model is the part of a dashboard model the tests read.
type model struct {
	UID         string `json:"uid"`
	Title       string `json:"title"`
	Annotations struct {
		List []struct {
			Datasource datasourceRef `json:"datasource"`
			Target     struct {
				Type string   `json:"type"`
				Tags []string `json:"tags"`
			} `json:"target"`
		} `json:"list"`
	} `json:"annotations"`
	Templating struct {
		List []struct {
			Type       string         `json:"type"`
			Datasource *datasourceRef `json:"datasource"`
			Definition string         `json:"definition"`
		} `json:"list"`
	} `json:"templating"`
	Panels []struct {
		Type       string         `json:"type"`
		Title      string         `json:"title"`
		GridPos    gridPos        `json:"gridPos"`
		Datasource *datasourceRef `json:"datasource"`
		Targets    []target       `json:"targets"`
	} `json:"panels"`
}

type scope struct {
	name       string
	dashboards []Dashboard
	uids       []string
	// filters are what every query must contain, by data source.
	metrics, logs, traces string
	annotation            string
}

func scopes() []scope {
	svc := func(kind string, templates ...string) scope {
		id := (kind + "000000000000")[:12]
		var uids []string
		for _, t := range templates {
			uids = append(uids, t+"-"+id)
		}
		return scope{
			name: "service " + kind, dashboards: ServiceDashboards(id, kind), uids: uids,
			metrics: `shed_service_id="` + id + `"`, logs: `shed.service.id="` + id + `"`,
			traces: "resource_attr:shed.service.id=" + id, annotation: "service:" + id,
		}
	}
	return []scope{
		svc("app", "svc-overview", "svc-logs", "svc-http", "svc-traces"),
		svc("postgres", "svc-overview", "svc-logs", "svc-postgres"),
		svc("mysql", "svc-overview", "svc-logs", "svc-mysql"),
		svc("mongo", "svc-overview", "svc-logs", "svc-mongo"),
		svc("redis", "svc-overview", "svc-logs", "svc-redis"),
		{
			name: "project", dashboards: ProjectDashboards("mnopqrstuvwx"), uids: []string{"proj-overview-mnopqrstuvwx"},
			metrics: `shed_project_id="mnopqrstuvwx"`, logs: `shed.project.id="mnopqrstuvwx"`, annotation: "project:mnopqrstuvwx",
		},
		{
			name: "shed", dashboards: ShedDashboards(), annotation: "shed",
			uids: []string{
				"shed-overview", "shed-deploys", "shed-api", "shed-proxy", "shed-host",
				"shed-backups", "shed-runtime", "shed-telemetry", "shed-security",
			},
		},
	}
}

var (
	// selectorRE matches a metric selector such as name{a="b"}.
	selectorRE = regexp.MustCompile(`[a-zA-Z_:][a-zA-Z0-9_:]*\{[^}]*\}`)
	// metricRE matches a name from one of the metric families the
	// templates use.
	metricRE = regexp.MustCompile(`\b(shed|pg|mysql|mongodb|redis|go|vm|vl|vt)_[a-zA-Z0-9_]+\b`)
	// labelListRE and stringRE match label lists and strings, which hold
	// label names and values rather than metric names.
	labelListRE = regexp.MustCompile(`\b(by|on|without|group_left|group_right) \([^)]*\)`)
	stringRE    = regexp.MustCompile(`"[^"]*"`)
)

// unfilteredMetrics returns the metric names in expr that are not in a
// selector containing filter.
func unfilteredMetrics(expr, filter string) []string {
	var bad []string
	for _, s := range selectorRE.FindAllString(expr, -1) {
		if !strings.Contains(s, filter) {
			bad = append(bad, s)
		}
	}
	rest := selectorRE.ReplaceAllString(expr, "")
	rest = labelListRE.ReplaceAllString(rest, "")
	rest = stringRE.ReplaceAllString(rest, "")
	return append(bad, metricRE.FindAllString(rest, -1)...)
}

func TestUnfilteredMetrics(t *testing.T) {
	f := `shed_service_id="x"`
	tests := []struct {
		expr string
		bad  int
	}{
		{`sum by (shed_service_name) (rate(shed_http_requests_total{shed_service_id="x",code=~"5.."}[5m]))`, 0},
		{`max(redis_memory_used_bytes{shed_service_id="y"})`, 1},
		{`max(redis_memory_used_bytes)`, 1},
		{`label_replace(rate(shed_http_requests_total{shed_service_id="x"}[1m]), "class", "${1}xx", "code", "([1-5])..")`, 0},
	}
	for _, tt := range tests {
		if got := unfilteredMetrics(tt.expr, f); len(got) != tt.bad {
			t.Errorf("unfilteredMetrics(%s) = %v, want %d", tt.expr, got, tt.bad)
		}
	}
}

func TestTemplates(t *testing.T) {
	known := map[datasourceRef]bool{metricsDS: true, logsDS: true, tracesDS: true}
	seen := map[string]bool{}
	for _, sc := range scopes() {
		t.Run(sc.name, func(t *testing.T) {
			if len(sc.dashboards) != len(sc.uids) {
				t.Fatalf("got %d dashboards, want %d", len(sc.dashboards), len(sc.uids))
			}
			for i, d := range sc.dashboards {
				var m model
				if err := json.Unmarshal(d.JSON, &m); err != nil {
					t.Fatalf("%s: invalid JSON: %v", d.UID, err)
				}
				if d.UID != sc.uids[i] || m.UID != d.UID || !strings.HasPrefix(d.UID, d.Template) {
					t.Errorf("dashboard %d: uid %q, model uid %q, template %q, want %q", i, d.UID, m.UID, d.Template, sc.uids[i])
				}
				if len(d.UID) > 40 {
					t.Errorf("%s: uid longer than 40 characters", d.UID)
				}
				if d.Version < 1 || m.Title == "" || len(m.Panels) == 0 {
					t.Errorf("%s: version %d, title %q, %d panels", d.UID, d.Version, m.Title, len(m.Panels))
				}
				if seen[d.UID] {
					t.Errorf("%s: duplicate uid", d.UID)
				}
				seen[d.UID] = true

				ann := m.Annotations.List
				if len(ann) != 1 || ann[0].Datasource != grafanaDS || ann[0].Target.Type != "tags" ||
					len(ann[0].Target.Tags) != 1 || ann[0].Target.Tags[0] != sc.annotation {
					t.Errorf("%s: annotation queries %+v, want tag %q", d.UID, ann, sc.annotation)
				}
				for _, v := range m.Templating.List {
					if v.Type == "query" && sc.metrics != "" && len(unfilteredMetrics(v.Definition, sc.metrics)) > 0 {
						t.Errorf("%s: variable query %q is not filtered by %s", d.UID, v.Definition, sc.metrics)
					}
				}
				checkLayout(t, d.UID, m)
				for _, p := range m.Panels {
					if p.Type == "row" {
						continue
					}
					if len(p.Targets) == 0 {
						t.Errorf("%s / %s: no queries", d.UID, p.Title)
					}
					for _, q := range p.Targets {
						where := d.UID + " / " + p.Title + " / " + q.RefID
						if !known[q.Datasource] {
							t.Errorf("%s: unknown data source %+v", where, q.Datasource)
						}
						if p.Datasource == nil || (*p.Datasource != q.Datasource && *p.Datasource != mixedDS) {
							t.Errorf("%s: panel data source %+v, query %+v", where, p.Datasource, q.Datasource)
						}
						checkFilter(t, where, sc, q)
					}
				}
			}
		})
	}
}

func checkFilter(t *testing.T, where string, sc scope, q target) {
	t.Helper()
	switch q.Datasource {
	case metricsDS:
		if q.Expr == "" {
			t.Errorf("%s: empty query", where)
		}
		if sc.metrics != "" {
			if bad := unfilteredMetrics(q.Expr, sc.metrics); len(bad) > 0 {
				t.Errorf("%s: %v not filtered by %s in %s", where, bad, sc.metrics, q.Expr)
			}
		}
	case logsDS:
		if !strings.HasPrefix(q.Expr, "{") {
			t.Errorf("%s: LogsQL %q does not start with a stream filter", where, q.Expr)
		}
		if sc.logs != "" && !strings.HasPrefix(q.Expr, "{"+sc.logs+",") && !strings.HasPrefix(q.Expr, "{"+sc.logs+"}") {
			t.Errorf("%s: LogsQL %q is not inside stream %s", where, q.Expr, sc.logs)
		}
		if strings.Contains(q.Expr, "| stats") != (q.QueryType != "instant") {
			t.Errorf("%s: query type %q for %q", where, q.QueryType, q.Expr)
		}
	case tracesDS:
		if q.QueryType != "search" || q.Service == "" || (sc.traces != "" && q.Tags != sc.traces) {
			t.Errorf("%s: trace search %+v, want tags %q", where, q, sc.traces)
		}
	}
}

// checkLayout fails if panels leave the 24-column grid or overlap.
func checkLayout(t *testing.T, uid string, m model) {
	t.Helper()
	for i, a := range m.Panels {
		g := a.GridPos
		if g.X < 0 || g.W <= 0 || g.H <= 0 || g.X+g.W > 24 {
			t.Errorf("%s / %s: outside the grid: %+v", uid, a.Title, g)
		}
		for _, b := range m.Panels[:i] {
			h := b.GridPos
			if g.X < h.X+h.W && h.X < g.X+g.W && g.Y < h.Y+h.H && h.Y < g.Y+g.H {
				t.Errorf("%s: %q overlaps %q", uid, a.Title, b.Title)
			}
		}
	}
}

func TestServiceDashboardsUnknownKind(t *testing.T) {
	var got []string
	for _, d := range ServiceDashboards("abc", "something-new") {
		got = append(got, d.Template)
	}
	if strings.Join(got, ",") != "svc-overview,svc-logs" {
		t.Errorf("templates for an unknown kind = %v", got)
	}
}
