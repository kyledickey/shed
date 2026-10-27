package control

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

type fakeLogsStore struct {
	queries []string
	start   []time.Time
	end     []time.Time
	limits  []int
	answer  func(q string) []victoria.Record
}

func (f *fakeLogsStore) Query(_ context.Context, q string, start, end time.Time, limit int) ([]victoria.Record, error) {
	f.queries = append(f.queries, q)
	f.start, f.end, f.limits = append(f.start, start), append(f.end, end), append(f.limits, limit)
	if f.answer == nil {
		return nil, nil
	}
	return f.answer(q), nil
}

type fakeMetricsStore struct {
	queries []string
	answer  func(q string) *victoria.Result
}

func (f *fakeMetricsStore) Query(_ context.Context, q string, _ time.Time) (*victoria.Result, error) {
	f.queries = append(f.queries, q)
	return f.answer(q), nil
}

func (f *fakeMetricsStore) QueryRange(ctx context.Context, q string, _, _ time.Time, _ time.Duration) (*victoria.Result, error) {
	return f.Query(ctx, q, time.Time{})
}

type telFixture struct {
	*fixture
	logs    *fakeLogsStore
	metrics *fakeMetricsStore
	svc     store.Service
}

func newTelFixture(t *testing.T) *telFixture {
	f := newFixture(t)
	tf := &telFixture{fixture: f, logs: &fakeLogsStore{}, metrics: &fakeMetricsStore{}, svc: createApp(t, f.st)}
	f.plane.stores = TelemetryStores{Metrics: tf.metrics, Logs: tf.logs}
	f.tel.s.Enabled = true
	return tf
}

var skeletonLiteral = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

func TestTelemetryOff(t *testing.T) {
	f := newTelFixture(t)
	f.tel.s.Enabled = false
	ctx := context.Background()
	calls := map[string]func() error{
		"QueryMetrics": func() error { _, err := f.plane.QueryMetrics(ctx, MetricsQuery{Query: "up"}); return err },
		"QueryLogs":    func() error { _, err := f.plane.QueryLogs(ctx, "*", time.Time{}, time.Time{}, 0); return err },
		"LogHistory":   func() error { _, err := f.plane.LogHistory(ctx, f.svc.ID, LogQuery{}); return err },
		"ErrorLogs":    func() error { _, err := f.plane.ErrorLogs(ctx, f.svc.ID, time.Time{}, 0); return err },
		"SlowRequests": func() error { _, err := f.plane.SlowRequests(ctx, f.svc.ID, time.Time{}, 0); return err },
		"SearchTraces": func() error { _, err := f.plane.SearchTraces(ctx, "", victoria.SearchParams{}); return err },
		"Trace":        func() error { _, err := f.plane.Trace(ctx, strings.Repeat("a", 32)); return err },
		"TraceURL":     func() error { _, err := f.plane.TraceExploreURL(ctx, strings.Repeat("a", 32)); return err },
	}
	for _, call := range calls {
		wantKind(t, call(), ErrConflict, "telemetry is off")
	}
	if len(f.logs.queries)+len(f.metrics.queries) != 0 {
		t.Errorf("stores were asked while telemetry is off: %v %v", f.logs.queries, f.metrics.queries)
	}
}

func TestLogHistory(t *testing.T) {
	f := newTelFixture(t)
	ctx := context.Background()
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"TOKEN": "hunter2-hunter2"}); err != nil {
		t.Fatal(err)
	}
	f.logs.answer = func(string) []victoria.Record {
		return []victoria.Record{
			{"_time": "2026-05-01T12:00:03.5Z", "_msg": "token=hunter2-hunter2 failed", "level": "ERROR", "shed.source": "container",
				"stream": "stderr", "shed.deployment.id": "dep1", "trace_id": "abc", "shed.service.id": f.svc.ID, "_stream": "{x}", "user": "u1"},
			{"_time": "2026-05-01T12:00:02Z", "_msg": "otlp", "severity": "WARN", "shed.source": "otlp"},
		}
	}
	page, err := f.plane.LogHistory(ctx, f.svc.ID, LogQuery{Search: "timeout", Level: "error", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 2 || page.Next != "2026-05-01T12:00:02Z" {
		t.Fatalf("page = %+v", page)
	}
	r := page.Records[0]
	if r.Message != "token=*** failed" || r.Level != "error" || r.Source != "container" || r.Stream != "stderr" ||
		r.DeploymentID != "dep1" || r.TraceID != "abc" || len(r.Fields) != 1 || r.Fields["user"] != "u1" {
		t.Errorf("record = %+v", r)
	}
	if r := page.Records[1]; r.Level != "warn" || r.Source != "otlp" {
		t.Errorf("record = %+v", r)
	}
	q := f.logs.queries[0]
	if !strings.HasPrefix(q, `_stream:{"shed.service.id"="`+f.svc.ID+`"} AND "timeout" AND (`) || f.logs.limits[0] != 2 {
		t.Errorf("query %s limit %d", q, f.logs.limits[0])
	}

	// A short page is the last one; the cursor excludes what was seen.
	f.logs.answer = func(string) []victoria.Record { return nil }
	cursor := time.Date(2026, 5, 1, 12, 0, 2, 0, time.UTC)
	to := cursor.Add(time.Hour)
	page, err = f.plane.LogHistory(ctx, f.svc.ID, LogQuery{Before: cursor, To: to})
	if err != nil || page.Next != "" || len(page.Records) != 0 {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if got := f.logs.end[1]; !got.Equal(cursor.Add(-time.Nanosecond)) || f.logs.limits[1] != DefaultHistoryRecords {
		t.Errorf("end %v limit %d", got, f.logs.limits[1])
	}

	for _, in := range []LogQuery{{Level: "loud"}, {Search: `"open`}} {
		_, err := f.plane.LogHistory(ctx, f.svc.ID, in)
		wantKind(t, err, ErrInvalid, "")
	}
	if _, err := f.plane.LogHistory(ctx, "nope", LogQuery{}); err == nil {
		t.Error("history of an unknown service")
	}
}

// TestLogHistoryStaysInsideTheService feeds hostile searches and checks that
// the query sent to the store is the service's stream filter followed only by
// quoted literals joined with AND, so no search can reach another service.
func TestLogHistoryStaysInsideTheService(t *testing.T) {
	f := newTelFixture(t)
	ctx := context.Background()
	shape := regexp.MustCompile(`^_stream:\{Q=Q\}( AND (Q|Q:Q))*( AND \((Q:i\(Q\)( OR )?)+\))?$`)
	attacks := []string{
		`x" OR "a"="a`,
		`x") OR ("1"="1`,
		`foo | delete`,
		`a or b`,
		`) OR (`,
		`_stream:{"shed.service.id"="other"}`,
		`{shed.service.id="other"}`,
		`* | limit 1000000`,
		`level:error or shed.service.id:other`,
		`"x\" OR \"y"`,
		`shed.service.id:"x\" OR *"`,
		"x\n| drop _msg",
	}
	for _, a := range attacks {
		for _, level := range []string{"", "error"} {
			f.logs.queries = nil
			_, err := f.plane.LogHistory(ctx, f.svc.ID, LogQuery{Search: a, Level: level})
			if err != nil {
				continue
			}
			q := f.logs.queries[0]
			if !strings.HasPrefix(q, `_stream:{"shed.service.id"="`+f.svc.ID+`"}`) {
				t.Errorf("search %q: query %s does not start with the service's stream filter", a, q)
			}
			if sk := skeletonLiteral.ReplaceAllString(q, "Q"); !shape.MatchString(sk) {
				t.Errorf("search %q: query %s has skeleton %s", a, q, sk)
			}
		}
	}
}

func TestQueryLogsMasksAndCaps(t *testing.T) {
	f := newTelFixture(t)
	ctx := context.Background()
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"KEY": "sk-live-123456789"}); err != nil {
		t.Fatal(err)
	}
	f.logs.answer = func(string) []victoria.Record {
		return []victoria.Record{{"_msg": "auth sk-live-123456789", "other": "sk-live-123456789!"}}
	}
	got, err := f.plane.QueryLogs(ctx, "error", time.Time{}, time.Time{}, 99999)
	if err != nil {
		t.Fatal(err)
	}
	if got[0]["_msg"] != "auth ***" || got[0]["other"] != "***!" {
		t.Errorf("records = %v", got)
	}
	if f.logs.limits[0] != MaxLogRecords {
		t.Errorf("limit %d, want %d", f.logs.limits[0], MaxLogRecords)
	}
	if _, err := f.plane.QueryLogs(ctx, "  ", time.Time{}, time.Time{}, 0); err == nil {
		t.Error("blank query accepted")
	}
}

func TestSlowRequestsAndErrorLogs(t *testing.T) {
	f := newTelFixture(t)
	ctx := context.Background()
	f.logs.answer = func(q string) []victoria.Record {
		if strings.Contains(q, "| stats") {
			return []victoria.Record{{"p50": "0.01", "p95": "0.25", "p99": "1.5"}}
		}
		return []victoria.Record{{"_time": "2026-05-01T12:00:00Z", "method": "GET", "host": "a.example", "path": "/slow", "status": "200",
			"duration": "1.25", "client_ip": "203.0.113.9", "trace_id": "t1"}}
	}
	got, err := f.plane.SlowRequests(ctx, f.svc.ID, time.Time{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	edge := `_stream:{"shed.service.id"="` + f.svc.ID + `","shed.source"="edge"}`
	if !strings.HasPrefix(f.logs.queries[0], edge+" | stats") || f.logs.queries[1] != edge+" | sort by (duration desc) | limit 5" {
		t.Errorf("queries %q", f.logs.queries)
	}
	want := SlowRequest{Time: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Method: "GET", Host: "a.example", Path: "/slow", Status: 200, DurationMs: 1250, ClientIP: "203.0.113.9", TraceID: "t1"}
	if got.P50Ms != 10 || got.P95Ms != 250 || got.P99Ms != 1500 || len(got.Requests) != 1 || got.Requests[0] != want {
		t.Errorf("got %+v", got)
	}

	f.logs.queries = nil
	if _, err := f.plane.ErrorLogs(ctx, f.svc.ID, time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if q := f.logs.queries[0]; !strings.HasPrefix(q, `_stream:{"shed.service.id"="`+f.svc.ID+`"} AND ("level":i("error")`) {
		t.Errorf("error logs query %s", q)
	}
}

func TestTopQueries(t *testing.T) {
	f := newTelFixture(t)
	ctx := context.Background()
	db, err := f.st.CreateService(ctx, store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	vec := func(vals map[string]float64) *victoria.Result {
		res := &victoria.Result{Type: "vector"}
		for q, v := range vals {
			res.Series = append(res.Series, victoria.Series{Labels: map[string]string{"query": q}, Values: []victoria.Sample{{Value: v}}})
		}
		return res
	}
	f.metrics.answer = func(q string) *victoria.Result {
		switch {
		case strings.Contains(q, "seconds_total"):
			return vec(map[string]float64{"SELECT 1": 2, "SELECT slow": 30, "SELECT mid": 5})
		case strings.Contains(q, "calls_total"):
			return vec(map[string]float64{"SELECT 1": 1000, "SELECT slow": 10, "SELECT mid": 5})
		}
		return vec(map[string]float64{"SELECT 1": 1000, "SELECT slow": 500})
	}
	got, err := f.plane.TopQueries(ctx, db.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []QueryStat{
		{Query: "SELECT slow", Calls: 10, TotalMs: 30000, MeanMs: 3000, Rows: 500},
		{Query: "SELECT mid", Calls: 5, TotalMs: 5000, MeanMs: 1000},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if q := f.metrics.queries[0]; !strings.Contains(q, `shed_service_id="`+db.ID+`"`) || !strings.Contains(q, "group_left (query) max by (queryid, query) (pg_stat_statements_query_id{") {
		t.Errorf("query %s", f.metrics.queries[0])
	}
	_, err = f.plane.TopQueries(ctx, f.svc.ID, 0)
	wantKind(t, err, ErrInvalid, "postgres and mysql")
}

func TestTraceExploreURL(t *testing.T) {
	f := newTelFixture(t)
	id := "4bf92f3577b34da6a3ce929d0e0e4736"
	got, err := f.plane.TraceExploreURL(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	path, query, _ := strings.Cut(got, "?")
	if path != "/grafana/explore" {
		t.Fatalf("path %s", path)
	}
	v, err := url.ParseQuery(query)
	if err != nil || v.Get("schemaVersion") != "1" || v.Get("orgId") != "1" {
		t.Fatalf("query %s: %v", query, err)
	}
	var panes map[string]struct {
		Datasource string `json:"datasource"`
		Queries    []struct {
			RefID      string            `json:"refId"`
			Query      string            `json:"query"`
			Datasource map[string]string `json:"datasource"`
		} `json:"queries"`
		Range map[string]string `json:"range"`
	}
	if err := json.Unmarshal([]byte(v.Get("panes")), &panes); err != nil || len(panes) != 1 {
		t.Fatalf("panes %s: %v", v.Get("panes"), err)
	}
	for _, p := range panes {
		if p.Datasource != "shed-traces" || len(p.Queries) != 1 || p.Queries[0].Query != id ||
			p.Queries[0].Datasource["uid"] != "shed-traces" || p.Queries[0].Datasource["type"] != "jaeger" ||
			p.Range["from"] != "now-14d" || p.Range["to"] != "now" {
			t.Errorf("pane %+v", p)
		}
	}
	for _, bad := range []string{"", "xyz", strings.ToUpper(id), id + "0", id + `"},"x":{"`} {
		_, err := f.plane.TraceExploreURL(context.Background(), bad)
		wantKind(t, err, ErrInvalid, "trace ID")
	}
}
