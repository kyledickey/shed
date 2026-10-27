package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
	"github.com/kyledickey/shed/internal/victoria"
)

func TestToolCallsAreTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Install(t)
	agent := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithActor(r.Context(), audit.Actor{Kind: audit.Agent, ClientID: "c1", ClientName: "Claude"})
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	cs := connectWith(t, &fakeBackend{}, agent)
	call(t, cs, "get_service", map[string]any{"service_id": "secret-looking-id"})

	s := rec.Span("tools/call get_service")
	if s == nil {
		t.Fatal("no span for the tool call")
	}
	for _, kv := range s.Attributes() {
		if kv.Value.String() == "secret-looking-id" {
			t.Errorf("tool span records the argument %s", kv.Key)
		}
	}
	if n := rec.Sum(t, "shed_mcp_tool_calls_total",
		attribute.String("tool", "get_service"), attribute.String("client", "Claude")); n != 1 {
		t.Errorf("shed_mcp_tool_calls_total{get_service, Claude} = %d, want 1", n)
	}
}

// telState is what the fake backend answers the telemetry tools with and
// what it was asked.
type telState struct {
	off     bool
	result  *victoria.Result
	records []victoria.Record
	trace   *victoria.Trace

	metricsQuery control.MetricsQuery
	logs         struct {
		query      string
		start, end time.Time
		limit      int
	}
	search  victoria.SearchParams
	service string
	since   time.Time
	limit   int
	folder  string
}

var errOff = fmt.Errorf("%w", observability.ErrDisabled)

func (f *fakeBackend) gate() error {
	if f.fail != nil {
		return f.fail
	}
	if f.tel.off {
		return errOff
	}
	return nil
}

func (f *fakeBackend) Telemetry(context.Context) (observability.Status, error) {
	st := observability.Status{Settings: observability.Settings{Enabled: !f.tel.off}}
	if !f.tel.off {
		st.Components = []observability.ComponentStatus{{Name: observability.Metrics, State: observability.Running, Version: "v1.155.0"}}
		st.Ingest = observability.Ingest{Spans: 3, Logs: 40, Points: 100}
		st.Dropped = []observability.Drop{{Reason: "rate_cap", Signal: "logs", Count: 12}}
	}
	return st, nil
}

func (f *fakeBackend) QueryMetrics(_ context.Context, q control.MetricsQuery) (*victoria.Result, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	f.tel.metricsQuery = q
	return f.tel.result, nil
}

func (f *fakeBackend) QueryLogs(_ context.Context, q string, start, end time.Time, limit int) ([]victoria.Record, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	f.tel.logs.query, f.tel.logs.start, f.tel.logs.end, f.tel.logs.limit = q, start, end, limit
	return f.tel.records, nil
}

func (f *fakeBackend) SearchTraces(_ context.Context, id string, sp victoria.SearchParams) ([]victoria.TraceSummary, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	if id != "" && id != "svc1" {
		return nil, store.ErrNotFound
	}
	f.tel.service, f.tel.search = id, sp
	return []victoria.TraceSummary{{TraceID: "t1", Service: "web", Name: "GET /", Start: created, Duration: 1500 * time.Microsecond, Spans: 3, Errors: 1}}, nil
}

func (f *fakeBackend) Trace(_ context.Context, id string) (*victoria.Trace, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	if f.tel.trace == nil || id != f.tel.trace.TraceID {
		return nil, store.ErrNotFound
	}
	return f.tel.trace, nil
}

func (f *fakeBackend) ErrorLogs(_ context.Context, id string, since time.Time, limit int) ([]victoria.Record, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	if err := f.check(id, "svc1"); err != nil {
		return nil, err
	}
	f.tel.since, f.tel.limit = since, limit
	return f.tel.records, nil
}

func (f *fakeBackend) SlowRequests(_ context.Context, id string, since time.Time, limit int) (control.SlowRequests, error) {
	if err := f.gate(); err != nil {
		return control.SlowRequests{}, err
	}
	if err := f.check(id, "svc1"); err != nil {
		return control.SlowRequests{}, err
	}
	f.tel.since, f.tel.limit = since, limit
	return control.SlowRequests{P50Ms: 10.123, P95Ms: 250, P99Ms: 1500, Requests: []control.SlowRequest{
		{Time: created, Method: "GET", Host: "web.example.com", Path: "/slow", Status: 200, DurationMs: 1250.456, ClientIP: "203.0.113.9", TraceID: "t1"},
	}}, nil
}

func (f *fakeBackend) TopQueries(_ context.Context, id string, _ int) ([]control.QueryStat, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	if id != "db1" {
		return nil, &control.Error{Kind: control.ErrInvalid, Msg: "top_queries covers postgres and mysql services, not app"}
	}
	return []control.QueryStat{{Query: "SELECT $1", Calls: 10, TotalMs: 3000, MeanMs: 300, Rows: 10}}, nil
}

func (f *fakeBackend) Dashboards(_ context.Context, folder string) ([]control.Dashboard, error) {
	if err := f.gate(); err != nil {
		return nil, err
	}
	f.tel.folder = folder
	return []control.Dashboard{{UID: "svc-overview-svc1", Title: "Overview", Folder: "web", URL: "https://shed.example.com/grafana/d/svc-overview-svc1"}}, nil
}

func TestTelemetryTools(t *testing.T) {
	f := &fakeBackend{}
	f.tel.result = &victoria.Result{Type: "vector", Series: []victoria.Series{
		{Labels: map[string]string{"shed_service_id": "svc1"}, Values: []victoria.Sample{{Time: created, Value: 12.5}}},
		{Labels: map[string]string{"x": "nan"}, Values: []victoria.Sample{{Time: created, Value: math.NaN()}, {Time: created.Add(time.Second), Value: math.Inf(-1)}}},
	}}
	f.tel.records = []victoria.Record{{"_time": "2026-01-02T03:04:05Z", "_msg": "boom", "level": "error"}}
	f.tel.trace = &victoria.Trace{TraceID: strings.Repeat("a", 32), Spans: []victoria.Span{
		{SpanID: "s1", Service: "web", Name: "GET /", Kind: "server", Start: created, Duration: 2 * time.Millisecond, Status: "error", Attributes: map[string]string{"http.method": "GET"}},
		{SpanID: "s2", ParentID: "s1", Service: "web", Name: "db", Kind: "client", Start: created, Duration: time.Millisecond, Status: "ok"},
	}}
	cs := connect(t, f)

	tests := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"telemetry_status", nil, []string{`"enabled":true`, `"name":"metrics"`, `"state":"running"`, `"version":"v1.155.0"`, `"spansPerSecond":3`, `"reason":"rate_cap"`}},
		{"query_metrics", map[string]any{"query": "up"}, []string{
			`"resultType":"vector"`, `"labels":{"shed_service_id":"svc1"}`, `"values":[[1767323045,12.5]]`,
			`"values":[[1767323045,null],[1767323046,null]]`,
		}},
		{"query_logs", map[string]any{"query": "error", "start": "15m", "limit": 50}, []string{`"_msg":"boom"`, `"level":"error"`}},
		{"search_traces", map[string]any{"service_id": "svc1", "operation": "GET /", "min_duration": "250ms", "tags": map[string]any{"http.status_code": "500"}, "start": "1h"},
			[]string{`"traceId":"t1"`, `"durationMs":1.5`, `"spans":3`, `"errors":1`}},
		{"get_trace", map[string]any{"trace_id": strings.Repeat("a", 32)}, []string{`"spanId":"s1"`, `"parentId":"s1"`, `"status":"error"`, `"http.method":"GET"`, `"durationMs":2`}},
		{"error_logs", map[string]any{"service_id": "svc1", "since": "2h", "limit": 5}, []string{`"_msg":"boom"`}},
		{"slow_requests", map[string]any{"service_id": "svc1"}, []string{`"p50Ms":10.12`, `"p99Ms":1500`, `"durationMs":1250.46`, `"clientIp":"203.0.113.9"`, `"traceId":"t1"`}},
		{"top_queries", map[string]any{"service_id": "db1"}, []string{`"query":"SELECT $1"`, `"calls":10`, `"totalMs":3000`, `"meanMs":300`}},
		{"list_dashboards", map[string]any{"folder_uid": "s-svc1"}, []string{`"uid":"svc-overview-svc1"`, `"folder":"web"`, `"url":"https://shed.example.com/grafana/d/svc-overview-svc1"`}},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			text, isErr := call(t, cs, tt.tool, tt.args)
			if isErr {
				t.Fatalf("error: %s", text)
			}
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("result lacks %s:\n%s", w, text)
				}
			}
		})
	}

	if q := f.tel.metricsQuery; q.Query != "up" || !q.Start.IsZero() || !q.Time.IsZero() {
		t.Errorf("metrics query = %+v", q)
	}
	if l := f.tel.logs; l.query != "error" || l.limit != 50 || time.Since(l.start) < 14*time.Minute || time.Since(l.start) > 16*time.Minute || !l.end.IsZero() {
		t.Errorf("logs query = %+v", l)
	}
	sp := f.tel.search
	if f.tel.service != "svc1" || sp.Operation != "GET /" || sp.MinDuration != 250*time.Millisecond || sp.Tags["http.status_code"] != "500" ||
		time.Since(sp.Start) < 59*time.Minute {
		t.Errorf("search = %q %+v", f.tel.service, sp)
	}
	if f.tel.folder != "s-svc1" {
		t.Errorf("folder = %q", f.tel.folder)
	}
}

func TestTelemetryOffIsAnError(t *testing.T) {
	f := &fakeBackend{}
	f.tel.off = true
	cs := connect(t, f)
	for tool, args := range map[string]map[string]any{
		"query_metrics":   {"query": "up"},
		"query_logs":      {"query": "*"},
		"search_traces":   nil,
		"get_trace":       {"trace_id": strings.Repeat("a", 32)},
		"error_logs":      {"service_id": "svc1"},
		"slow_requests":   {"service_id": "svc1"},
		"top_queries":     {"service_id": "db1"},
		"list_dashboards": nil,
	} {
		text, isErr := call(t, cs, tool, args)
		if !isErr || !strings.Contains(text, "telemetry is off") {
			t.Errorf("%s while off: error %v, %q", tool, isErr, text)
		}
	}
	text, isErr := call(t, cs, "telemetry_status", nil)
	if isErr || !strings.Contains(text, `"enabled":false`) || !strings.Contains(text, `"components":[]`) {
		t.Errorf("telemetry_status while off: error %v, %q", isErr, text)
	}
}

func TestTelemetryToolErrors(t *testing.T) {
	f := &fakeBackend{}
	f.tel.result = &victoria.Result{Type: "vector"}
	cs := connect(t, f)
	tests := []struct {
		name, tool string
		args       map[string]any
		want       string
	}{
		{"bad time", "query_metrics", map[string]any{"query": "up", "time": "soon"}, "time must be an RFC 3339 time or a duration"},
		{"time and start", "query_metrics", map[string]any{"query": "up", "time": "1h", "start": "2h"}, "not both"},
		{"end without start", "query_metrics", map[string]any{"query": "up", "end": "1h"}, "give start too"},
		{"end before start", "query_metrics", map[string]any{"query": "up", "start": "1h", "end": "2h"}, "end must be after start"},
		{"bad step", "query_metrics", map[string]any{"query": "up", "start": "1h", "step": "often"}, "step must be a duration"},
		{"bad since", "error_logs", map[string]any{"service_id": "svc1", "since": "lately"}, "since must be"},
		{"bad min duration", "search_traces", map[string]any{"min_duration": "slow"}, "min_duration must be a duration"},
		{"unknown service", "search_traces", map[string]any{"service_id": "nope"}, `service "nope" not found`},
		{"unknown trace", "get_trace", map[string]any{"trace_id": strings.Repeat("b", 32)}, `trace "`},
		{"not a database", "top_queries", map[string]any{"service_id": "svc1"}, "postgres and mysql"},
		{"missing query", "query_logs", nil, "query"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, isErr := call(t, cs, tt.tool, tt.args)
			if !isErr || !strings.Contains(text, tt.want) {
				t.Errorf("%s = %q (error %v), want %q", tt.tool, text, isErr, tt.want)
			}
		})
	}
}

func TestQueryMetricsRange(t *testing.T) {
	f := &fakeBackend{}
	f.tel.result = &victoria.Result{Type: "matrix"}
	cs := connect(t, f)
	text, isErr := call(t, cs, "query_metrics", map[string]any{"query": "up", "start": "6h", "step": "5m"})
	if isErr {
		t.Fatal(text)
	}
	q := f.tel.metricsQuery
	if q.Step != 5*time.Minute || q.End.Sub(q.Start) < 5*time.Hour+59*time.Minute || !strings.Contains(text, `"stepSeconds":300`) {
		t.Errorf("query = %+v, result %s", q, text)
	}
	// A step that would give more than 500 points is raised.
	call(t, cs, "query_metrics", map[string]any{"query": "up", "start": "24h", "step": "1s"})
	if f.tel.metricsQuery.Step < 24*time.Hour/maxPoints {
		t.Errorf("step %v keeps too many points", f.tel.metricsQuery.Step)
	}
	// The default gives about 250 points.
	call(t, cs, "query_metrics", map[string]any{"query": "up", "start": "25h"})
	if st := f.tel.metricsQuery.Step; st < 5*time.Minute || st > 7*time.Minute {
		t.Errorf("default step %v", st)
	}
	call(t, cs, "query_metrics", map[string]any{"query": "up", "time": "30m"})
	if tm := f.tel.metricsQuery.Time; time.Since(tm) < 29*time.Minute || !f.tel.metricsQuery.Start.IsZero() {
		t.Errorf("instant query = %+v", f.tel.metricsQuery)
	}
}

func TestMetricsCaps(t *testing.T) {
	var series []victoria.Series
	for i := range maxSeries + 5 {
		vals := make([]victoria.Sample, maxPoints+7)
		for j := range vals {
			vals[j] = victoria.Sample{Time: created.Add(time.Duration(j) * time.Second), Value: float64(j)}
		}
		series = append(series, victoria.Series{Labels: map[string]string{"i": fmt.Sprint(i)}, Values: vals})
	}
	f := &fakeBackend{}
	f.tel.result = &victoria.Result{Type: "matrix", Series: series}
	text, isErr := call(t, connect(t, f), "query_metrics", map[string]any{"query": "up", "start": "1h"})
	if isErr {
		t.Fatal(text)
	}
	var out metricsOut
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Series) != maxSeries || len(out.Series[0].Values) != maxPoints || !out.Truncated {
		t.Errorf("%d series of %d points, truncated %v", len(out.Series), len(out.Series[0].Values), out.Truncated)
	}
	// The newest points are kept.
	if last := out.Series[0].Values[maxPoints-1]; *last[1] != float64(maxPoints+6) {
		t.Errorf("last value %v", *last[1])
	}
}

func TestQueryLogsBounds(t *testing.T) {
	f := &fakeBackend{}
	for i := range 400 {
		f.tel.records = append(f.tel.records, victoria.Record{"_msg": strings.Repeat("x", 1500) + fmt.Sprint(i), "i": fmt.Sprint(i)})
	}
	f.tel.records = append(f.tel.records, victoria.Record{"_msg": strings.Repeat("é", maxLogLineRunes+50)})
	text, isErr := call(t, connect(t, f), "query_logs", map[string]any{"query": "*", "limit": 99999})
	if isErr {
		t.Fatal(text)
	}
	if len(text) > maxLogBytes+200 {
		t.Errorf("result is %d bytes", len(text))
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		var rec map[string]string
		if err := json.Unmarshal([]byte(l), &rec); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "older records omitted") || !strings.HasPrefix(lines[0], `{"_msg":"xxx`) {
		t.Errorf("last line %q", last)
	}
	if f.tel.logs.limit != 99999 {
		t.Errorf("limit passed on = %d (the control plane clamps it)", f.tel.logs.limit)
	}

	f.tel.records = []victoria.Record{{"_msg": strings.Repeat("é", maxLogLineRunes+50)}}
	text, _ = call(t, connect(t, f), "error_logs", map[string]any{"service_id": "svc1"})
	if !strings.Contains(text, " [line cut]") {
		t.Errorf("long field kept whole: %d bytes", len(text))
	}
	f.tel.records = nil
	if text, _ = call(t, connect(t, f), "query_logs", map[string]any{"query": "*"}); text != "(no log records)" {
		t.Errorf("empty = %q", text)
	}
}

func TestTraceCap(t *testing.T) {
	tr := &victoria.Trace{TraceID: strings.Repeat("c", 32)}
	for i := range maxTraceSpans + 20 {
		tr.Spans = append(tr.Spans, victoria.Span{SpanID: fmt.Sprint(i), Start: created, Attributes: map[string]string{"long": strings.Repeat("y", 5000)}})
	}
	f := &fakeBackend{}
	f.tel.trace = tr
	text, isErr := call(t, connect(t, f), "get_trace", map[string]any{"trace_id": tr.TraceID})
	if isErr {
		t.Fatal(text)
	}
	var out traceOut
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Spans) != maxTraceSpans || out.Omitted != 20 || len(out.Spans[0].Attributes["long"]) > maxAttributeLen+10 {
		t.Errorf("%d spans, %d omitted, attribute %d bytes", len(out.Spans), out.Omitted, len(out.Spans[0].Attributes["long"]))
	}
}

func TestTelemetryCallsAreAudited(t *testing.T) {
	f := &fakeBackend{}
	f.tel.result = &victoria.Result{Type: "vector"}
	cs := connect(t, f)
	call(t, cs, "error_logs", map[string]any{"service_id": "svc1"})
	call(t, cs, "query_metrics", map[string]any{"query": "up"})
	if len(f.records) != 2 {
		t.Fatalf("recorded %d events", len(f.records))
	}
	e := f.records[0]
	args, _ := e.Detail["arguments"].(map[string]any)
	if e.Action != "mcp.call" || e.Detail["tool"] != "error_logs" || e.ServiceID != "svc1" || args["service_id"] != "svc1" {
		t.Errorf("event = %+v", e)
	}
	if args := f.records[1].Detail["arguments"].(map[string]any); args["query"] != "up" {
		t.Errorf("arguments = %v", args)
	}
}

func TestInstructionsDescribeTelemetry(t *testing.T) {
	ins := connect(t, &fakeBackend{}).InitializeResult().Instructions
	for _, want := range []string{"shed_service_id", "shed.service.id", "_stream:{", "shed_http_requests_total", "telemetry is off", "null"} {
		if !strings.Contains(ins, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
}
