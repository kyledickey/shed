package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/victoria"
)

// telemetryBackend is what the telemetry tools read. *control.Plane
// implements it. Every method except Telemetry fails with a conflict that
// reads "telemetry is off" while telemetry is off.
type telemetryBackend interface {
	Telemetry(ctx context.Context) (observability.Status, error)
	QueryMetrics(ctx context.Context, q control.MetricsQuery) (*victoria.Result, error)
	QueryLogs(ctx context.Context, logsql string, start, end time.Time, limit int) ([]victoria.Record, error)
	SearchTraces(ctx context.Context, serviceID string, sp victoria.SearchParams) ([]victoria.TraceSummary, error)
	Trace(ctx context.Context, traceID string) (*victoria.Trace, error)
	ErrorLogs(ctx context.Context, serviceID string, since time.Time, limit int) ([]victoria.Record, error)
	SlowRequests(ctx context.Context, serviceID string, since time.Time, limit int) (control.SlowRequests, error)
	TopQueries(ctx context.Context, serviceID string, limit int) ([]control.QueryStat, error)
	Dashboards(ctx context.Context, folderUID string) ([]observability.Dashboard, error)
}

// Bounds on what the telemetry tools return.
const (
	maxSeries       = 500
	maxPoints       = 500
	maxTraceSpans   = 1000
	maxAttributeLen = 1000 // runes of one span attribute
	defaultSince    = time.Hour
)

type queryMetricsArgs struct {
	Query string `json:"query" jsonschema:"a MetricsQL (PromQL superset) expression"`
	Time  string `json:"time,omitempty" jsonschema:"evaluate at this instant: RFC 3339 time, or a duration before now such as 15m; default now"`
	Start string `json:"start,omitempty" jsonschema:"with start, a range query: RFC 3339 time or a duration before now such as 6h"`
	End   string `json:"end,omitempty" jsonschema:"end of the range: RFC 3339 time or a duration before now; default now"`
	Step  string `json:"step,omitempty" jsonschema:"distance between points of a range query, such as 30s or 5m; default so that there are about 250 points"`
}

type queryLogsArgs struct {
	Query string `json:"query" jsonschema:"a LogsQL query, for example {\"shed.service.id\"=\"...\"} error | limit 20"`
	Start string `json:"start,omitempty" jsonschema:"earliest record: RFC 3339 time, or a duration before now such as 15m"`
	End   string `json:"end,omitempty" jsonschema:"latest record: RFC 3339 time, or a duration before now; default now"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many records to return, newest first; default 200, at most 2000"`
}

type searchTracesArgs struct {
	ServiceID   string            `json:"service_id,omitempty" jsonschema:"ID of the service whose traces to find"`
	Operation   string            `json:"operation,omitempty" jsonschema:"span name, for example GET /orders"`
	MinDuration string            `json:"min_duration,omitempty" jsonschema:"only traces at least this long, such as 250ms or 2s"`
	MaxDuration string            `json:"max_duration,omitempty" jsonschema:"only traces at most this long"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"span attributes that must have these values, for example {\"http.status_code\":\"500\"}"`
	Start       string            `json:"start,omitempty" jsonschema:"earliest start: RFC 3339 time, or a duration before now such as 1h"`
	End         string            `json:"end,omitempty" jsonschema:"latest start: RFC 3339 time, or a duration before now; default now"`
	Limit       int               `json:"limit,omitempty" jsonschema:"how many traces to return, newest first; default 20, at most 100"`
}

type traceArgs struct {
	TraceID string `json:"trace_id" jsonschema:"32 hexadecimal characters, from search_traces, a deployment's traceId, or a log record's trace_id"`
}

type errorLogsArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of the service, from list_projects or get_project"`
	Since     string `json:"since,omitempty" jsonschema:"RFC 3339 time, or a duration before now; default 1h"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many records to return, newest first; default 200, at most 2000"`
}

type slowRequestsArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of an app, from list_projects or get_project"`
	Since     string `json:"since,omitempty" jsonschema:"RFC 3339 time, or a duration before now; default 1h"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many of the slowest requests to return; default 20, at most 100"`
}

type dashboardsArgs struct {
	FolderUID string `json:"folder_uid,omitempty" jsonschema:"only the dashboards in this folder: s-<serviceID>, p-<projectID>, or shed"`
}

func (s *Server) addTelemetryTools() {
	add(s, "telemetry_status", "Telemetry status",
		"Reports whether telemetry is on, the state and version of its components, what the OTLP gateway ingests per second, and the records dropped in the last hour. Works while telemetry is off; every other telemetry tool then fails with \"telemetry is off\".",
		s.telemetryStatus)
	add(s, "query_metrics", "Query metrics",
		"Runs a MetricsQL query on VictoriaMetrics: instant (at time, default now) or, with start, over a range. Returns at most 500 series of 500 points. Values are numbers, or null where the result is NaN or infinite. See the instructions for metric names and labels.",
		s.queryMetrics)
	add(s, "query_logs", "Query logs",
		"Runs a LogsQL query on VictoriaLogs (container output, OTLP logs, edge access records, audit events) and returns the records as text, one JSON object per line, newest first, with variable values masked as ***. Add a stream filter to stay within one service; see the instructions.",
		s.queryLogs)
	add(s, "search_traces", "Search traces",
		"Finds traces in VictoriaTraces, newest first: trace ID, root service and span name, start, duration, span count, and error count. Narrow by service, operation, duration, attributes, and time.",
		s.searchTraces)
	add(s, "get_trace", "Get trace",
		"Returns the spans of one trace, by start time: IDs, parent, service, name, kind, start, duration, status (ok, error, unset), and attributes. At most 1000 spans.",
		s.getTrace)
	add(s, "error_logs", "Error logs",
		"Returns a service's error-level log records (container output and OTLP) since a time as text, one JSON object per line, newest first, with variable values masked as ***.",
		s.errorLogs)
	add(s, "slow_requests", "Slow requests",
		"Returns the p50, p95, and p99 request duration of an app since a time, and its slowest requests, from the edge access records: time, method, host, path, status, duration, client IP, and trace ID.",
		s.slowRequests)
	add(s, "top_queries", "Top queries",
		"Lists the statements of a Postgres or MySQL service that took the most time in total, with calls, total and mean milliseconds, and rows, from the statistics its exporter reports since the database started.",
		s.topQueries)
	add(s, "list_dashboards", "List dashboards",
		"Lists Grafana's dashboards with their URLs, optionally of one folder: s-<serviceID> for a service's, p-<projectID> for a project's, shed for shed's own.",
		s.listDashboards)
}

type telemetryStatusOut struct {
	Enabled    bool           `json:"enabled"`
	Components []componentOut `json:"components"`
	Ingest     ingestOut      `json:"ingest" jsonschema:"what the gateway accepted per second over the last minute"`
	Dropped    []dropOut      `json:"dropped" jsonschema:"records dropped in the last hour, by reason"`
}

type componentOut struct {
	Name    string `json:"name" jsonschema:"metrics, logs, traces, or grafana"`
	State   string `json:"state" jsonschema:"running, starting, stopped, or failed"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

type ingestOut struct {
	SpansPerSecond  float64 `json:"spansPerSecond"`
	LogsPerSecond   float64 `json:"logsPerSecond"`
	PointsPerSecond float64 `json:"pointsPerSecond"`
}

type dropOut struct {
	Reason string  `json:"reason" jsonschema:"rate_cap or series_cap"`
	Signal string  `json:"signal" jsonschema:"traces, metrics, or logs"`
	Count  float64 `json:"count"`
}

func (s *Server) telemetryStatus(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, telemetryStatusOut, error) {
	st, err := s.backend.Telemetry(ctx)
	if err != nil {
		return nil, telemetryStatusOut{}, s.fail(err, "", "")
	}
	out := telemetryStatusOut{
		Enabled:    st.Settings.Enabled,
		Components: make([]componentOut, 0, len(st.Components)),
		Ingest:     ingestOut{SpansPerSecond: st.Ingest.Spans, LogsPerSecond: st.Ingest.Logs, PointsPerSecond: st.Ingest.Points},
		Dropped:    make([]dropOut, 0, len(st.Dropped)),
	}
	for _, c := range st.Components {
		out.Components = append(out.Components, componentOut{Name: string(c.Name), State: string(c.State), Version: c.Version, Error: c.Error})
	}
	for _, d := range st.Dropped {
		out.Dropped = append(out.Dropped, dropOut{Reason: d.Reason, Signal: d.Signal, Count: d.Count})
	}
	return nil, out, nil
}

type metricsOut struct {
	ResultType  string      `json:"resultType" jsonschema:"vector (instant), matrix (range), or scalar"`
	StepSeconds float64     `json:"stepSeconds,omitempty" jsonschema:"distance between the points of a range query"`
	Series      []seriesOut `json:"series"`
	Truncated   bool        `json:"truncated,omitempty" jsonschema:"series or points were left out to stay within 500 series of 500 points; narrow the query"`
}

type seriesOut struct {
	Labels map[string]string `json:"labels"`
	// Values are [unix seconds, value] pairs; the value is null where it is
	// NaN or infinite, which JSON cannot carry.
	Values [][2]*float64 `json:"values" jsonschema:"[unix seconds, value] pairs; the value is null where the result is NaN or infinite"`
}

func (s *Server) queryMetrics(ctx context.Context, _ *sdk.CallToolRequest, in queryMetricsArgs) (*sdk.CallToolResult, metricsOut, error) {
	now := time.Now()
	q := control.MetricsQuery{Query: in.Query}
	var stepSeconds float64
	if in.Start == "" {
		if in.End != "" || in.Step != "" {
			return nil, metricsOut{}, fmt.Errorf("end and step apply to a range query; give start too")
		}
		if in.Time != "" {
			t, err := parseTime("time", in.Time, now)
			if err != nil {
				return nil, metricsOut{}, err
			}
			q.Time = t
		}
	} else {
		if in.Time != "" {
			return nil, metricsOut{}, fmt.Errorf("give time for an instant query, or start for a range query, not both")
		}
		var err error
		if q.Start, err = parseTime("start", in.Start, now); err != nil {
			return nil, metricsOut{}, err
		}
		q.End = now
		if in.End != "" {
			if q.End, err = parseTime("end", in.End, now); err != nil {
				return nil, metricsOut{}, err
			}
		}
		if !q.End.After(q.Start) {
			return nil, metricsOut{}, fmt.Errorf("end must be after start")
		}
		if q.Step, err = rangeStep(in.Step, q.End.Sub(q.Start)); err != nil {
			return nil, metricsOut{}, err
		}
		stepSeconds = q.Step.Seconds()
	}
	res, err := s.backend.QueryMetrics(ctx, q)
	if err != nil {
		return nil, metricsOut{}, s.fail(err, "", "")
	}
	return nil, toMetrics(res, stepSeconds), nil
}

// rangeStep parses the step of a range query, a duration or a number of
// seconds. It is at least a second, and at least what keeps the range within
// maxPoints points; without one, the range holds about 250 points.
func rangeStep(v string, span time.Duration) (time.Duration, error) {
	step := time.Duration(0)
	if v != "" {
		var err error
		if step, err = time.ParseDuration(v); err != nil {
			secs, serr := strconv.ParseFloat(v, 64)
			if serr != nil {
				return 0, fmt.Errorf("step must be a duration such as 30s or 5m, not %q", v)
			}
			step = time.Duration(secs * float64(time.Second))
		}
		if step <= 0 {
			return 0, fmt.Errorf("step must be positive")
		}
	} else {
		step = span / 250
	}
	step = max(step, span/maxPoints+1, time.Second)
	return step.Round(time.Second), nil
}

// toMetrics converts a query result for the wire, bounded to maxSeries
// series of the newest maxPoints points.
func toMetrics(res *victoria.Result, stepSeconds float64) metricsOut {
	out := metricsOut{ResultType: res.Type, StepSeconds: stepSeconds, Series: make([]seriesOut, 0, min(len(res.Series), maxSeries))}
	for i, ser := range res.Series {
		if i == maxSeries {
			out.Truncated = true
			break
		}
		vals := ser.Values
		if len(vals) > maxPoints {
			vals, out.Truncated = vals[len(vals)-maxPoints:], true
		}
		labels := ser.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		so := seriesOut{Labels: labels, Values: make([][2]*float64, 0, len(vals))}
		for _, v := range vals {
			t := float64(v.Time.UnixMilli()) / 1000
			var val *float64
			if !math.IsNaN(v.Value) && !math.IsInf(v.Value, 0) {
				val = &v.Value
			}
			so.Values = append(so.Values, [2]*float64{&t, val})
		}
		out.Series = append(out.Series, so)
	}
	return out
}

func (s *Server) queryLogs(ctx context.Context, _ *sdk.CallToolRequest, in queryLogsArgs) (*sdk.CallToolResult, any, error) {
	now := time.Now()
	var start, end time.Time
	var err error
	if in.Start != "" {
		if start, err = parseTime("start", in.Start, now); err != nil {
			return nil, nil, err
		}
	}
	if in.End != "" {
		if end, err = parseTime("end", in.End, now); err != nil {
			return nil, nil, err
		}
	}
	records, err := s.backend.QueryLogs(ctx, in.Query, start, end, in.Limit)
	if err != nil {
		return nil, nil, s.fail(err, "", "")
	}
	return recordsResult(records), nil, nil
}

func (s *Server) errorLogs(ctx context.Context, _ *sdk.CallToolRequest, in errorLogsArgs) (*sdk.CallToolResult, any, error) {
	since, err := sinceOrDefault(in.Since)
	if err != nil {
		return nil, nil, err
	}
	records, err := s.backend.ErrorLogs(ctx, in.ServiceID, since, in.Limit)
	if err != nil {
		return nil, nil, s.fail(err, "service", in.ServiceID)
	}
	return recordsResult(records), nil, nil
}

// sinceOrDefault parses since, an hour before now when empty.
func sinceOrDefault(v string) (time.Time, error) {
	now := time.Now()
	if v == "" {
		return now.Add(-defaultSince), nil
	}
	return parseTime("since", v, now)
}

// recordsResult returns log records as one text block, one JSON object per
// line in the order given. Field values longer than maxLogLineRunes are cut,
// and the oldest records are dropped to keep the text within maxLogBytes,
// with a line saying how many.
func recordsResult(records []victoria.Record) *sdk.CallToolResult {
	if len(records) == 0 {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "(no log records)"}}}
	}
	var b strings.Builder
	kept := 0
	for _, r := range records {
		cutRec := make(map[string]string, len(r))
		for k, v := range r {
			cutRec[k] = cut(v)
		}
		line, _ := json.Marshal(cutRec) // a string map always marshals
		if b.Len()+len(line)+1 > maxLogBytes {
			break
		}
		b.Write(line)
		b.WriteByte('\n')
		kept++
	}
	if dropped := len(records) - kept; dropped > 0 {
		fmt.Fprintf(&b, `{"_note":"%d older records omitted to bound the output"}`, dropped)
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.TrimRight(b.String(), "\n")}}}
}

type tracesOut struct {
	Traces []traceSummaryOut `json:"traces"`
}

type traceSummaryOut struct {
	TraceID    string    `json:"traceId"`
	Service    string    `json:"service" jsonschema:"service.name of the root span"`
	Name       string    `json:"name" jsonschema:"name of the root span"`
	Start      time.Time `json:"start"`
	DurationMs float64   `json:"durationMs"`
	Spans      int       `json:"spans"`
	Errors     int       `json:"errors,omitempty" jsonschema:"spans with an error status"`
}

func (s *Server) searchTraces(ctx context.Context, _ *sdk.CallToolRequest, in searchTracesArgs) (*sdk.CallToolResult, tracesOut, error) {
	now := time.Now()
	sp := victoria.SearchParams{Operation: in.Operation, Tags: in.Tags, Limit: in.Limit}
	var err error
	if in.MinDuration != "" {
		if sp.MinDuration, err = parseDuration("min_duration", in.MinDuration); err != nil {
			return nil, tracesOut{}, err
		}
	}
	if in.MaxDuration != "" {
		if sp.MaxDuration, err = parseDuration("max_duration", in.MaxDuration); err != nil {
			return nil, tracesOut{}, err
		}
	}
	if in.Start != "" {
		if sp.Start, err = parseTime("start", in.Start, now); err != nil {
			return nil, tracesOut{}, err
		}
	}
	if in.End != "" {
		if sp.End, err = parseTime("end", in.End, now); err != nil {
			return nil, tracesOut{}, err
		}
	}
	found, err := s.backend.SearchTraces(ctx, in.ServiceID, sp)
	if err != nil {
		return nil, tracesOut{}, s.fail(err, "service", in.ServiceID)
	}
	out := tracesOut{Traces: make([]traceSummaryOut, 0, len(found))}
	for _, t := range found {
		out.Traces = append(out.Traces, traceSummaryOut{
			TraceID: t.TraceID, Service: t.Service, Name: t.Name, Start: t.Start,
			DurationMs: ms(t.Duration), Spans: t.Spans, Errors: t.Errors,
		})
	}
	return nil, out, nil
}

type traceOut struct {
	Spans   []spanOut `json:"spans"`
	Omitted int       `json:"omitted,omitempty" jsonschema:"spans left out beyond 1000"`
}

type spanOut struct {
	SpanID     string            `json:"spanId"`
	ParentID   string            `json:"parentId,omitempty"`
	Service    string            `json:"service"`
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty" jsonschema:"internal, server, client, producer, or consumer"`
	Start      time.Time         `json:"start"`
	DurationMs float64           `json:"durationMs"`
	Status     string            `json:"status" jsonschema:"ok, error, or unset"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

func (s *Server) getTrace(ctx context.Context, _ *sdk.CallToolRequest, in traceArgs) (*sdk.CallToolResult, traceOut, error) {
	tr, err := s.backend.Trace(ctx, in.TraceID)
	if err != nil {
		return nil, traceOut{}, s.fail(err, "trace", in.TraceID)
	}
	n := min(len(tr.Spans), maxTraceSpans)
	out := traceOut{Spans: make([]spanOut, 0, n), Omitted: len(tr.Spans) - n}
	for _, sp := range tr.Spans[:n] {
		var attrs map[string]string
		if len(sp.Attributes) > 0 {
			attrs = make(map[string]string, len(sp.Attributes))
			for k, v := range sp.Attributes {
				attrs[k] = cutRunes(v, maxAttributeLen)
			}
		}
		out.Spans = append(out.Spans, spanOut{
			SpanID: sp.SpanID, ParentID: sp.ParentID, Service: sp.Service, Name: sp.Name, Kind: sp.Kind,
			Start: sp.Start, DurationMs: ms(sp.Duration), Status: sp.Status, Attributes: attrs,
		})
	}
	return nil, out, nil
}

type slowRequestsOut struct {
	P50Ms    float64          `json:"p50Ms"`
	P95Ms    float64          `json:"p95Ms"`
	P99Ms    float64          `json:"p99Ms"`
	Requests []slowRequestOut `json:"requests" jsonschema:"the slowest requests, slowest first"`
}

type slowRequestOut struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Host       string    `json:"host"`
	Path       string    `json:"path"`
	Status     int       `json:"status"`
	DurationMs float64   `json:"durationMs"`
	ClientIP   string    `json:"clientIp,omitempty"`
	TraceID    string    `json:"traceId,omitempty"`
}

func (s *Server) slowRequests(ctx context.Context, _ *sdk.CallToolRequest, in slowRequestsArgs) (*sdk.CallToolResult, slowRequestsOut, error) {
	since, err := sinceOrDefault(in.Since)
	if err != nil {
		return nil, slowRequestsOut{}, err
	}
	res, err := s.backend.SlowRequests(ctx, in.ServiceID, since, in.Limit)
	if err != nil {
		return nil, slowRequestsOut{}, s.fail(err, "service", in.ServiceID)
	}
	out := slowRequestsOut{P50Ms: round2(res.P50Ms), P95Ms: round2(res.P95Ms), P99Ms: round2(res.P99Ms), Requests: make([]slowRequestOut, 0, len(res.Requests))}
	for _, r := range res.Requests {
		out.Requests = append(out.Requests, slowRequestOut{
			Time: r.Time, Method: r.Method, Host: r.Host, Path: cutRunes(r.Path, maxAttributeLen), Status: r.Status,
			DurationMs: round2(r.DurationMs), ClientIP: r.ClientIP, TraceID: r.TraceID,
		})
	}
	return nil, out, nil
}

type topQueriesOut struct {
	Queries []queryStatOut `json:"queries" jsonschema:"statements by total time, longest first"`
}

type queryStatOut struct {
	Query   string  `json:"query" jsonschema:"the normalized statement"`
	Calls   float64 `json:"calls"`
	TotalMs float64 `json:"totalMs"`
	MeanMs  float64 `json:"meanMs"`
	Rows    float64 `json:"rows"`
}

func (s *Server) topQueries(ctx context.Context, _ *sdk.CallToolRequest, in serviceArgs) (*sdk.CallToolResult, topQueriesOut, error) {
	stats, err := s.backend.TopQueries(ctx, in.ServiceID, 0)
	if err != nil {
		return nil, topQueriesOut{}, s.fail(err, "service", in.ServiceID)
	}
	out := topQueriesOut{Queries: make([]queryStatOut, 0, len(stats))}
	for _, q := range stats {
		out.Queries = append(out.Queries, queryStatOut{
			Query: cut(q.Query), Calls: q.Calls, TotalMs: round2(q.TotalMs), MeanMs: round2(q.MeanMs), Rows: q.Rows,
		})
	}
	return nil, out, nil
}

type dashboardsOut struct {
	Dashboards []dashboardOut `json:"dashboards"`
}

type dashboardOut struct {
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Folder string `json:"folder,omitempty"`
	URL    string `json:"url" jsonschema:"opens in Grafana for a signed-in user"`
	// Customized marks a default dashboard a user saved; shed no longer
	// updates it.
	Customized bool `json:"customized,omitempty"`
	// Outdated marks a customized dashboard whose default changed since.
	Outdated bool `json:"outdated,omitempty"`
}

func (s *Server) listDashboards(ctx context.Context, _ *sdk.CallToolRequest, in dashboardsArgs) (*sdk.CallToolResult, dashboardsOut, error) {
	list, err := s.backend.Dashboards(ctx, in.FolderUID)
	if err != nil {
		return nil, dashboardsOut{}, s.fail(err, "", "")
	}
	out := dashboardsOut{Dashboards: make([]dashboardOut, 0, len(list))}
	for _, d := range list {
		out.Dashboards = append(out.Dashboards, dashboardOut{UID: d.UID, Title: d.Title, Folder: d.Folder, URL: d.URL, Customized: d.Customized, Outdated: d.Outdated})
	}
	return nil, out, nil
}

// parseDuration parses a positive duration such as 250ms or 2s.
func parseDuration(name, v string) (time.Duration, error) {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a duration such as 250ms or 2s, not %q", name, v)
	}
	return d, nil
}

func ms(d time.Duration) float64 { return round2(float64(d) / float64(time.Millisecond)) }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// cutRunes shortens v to at most n runes.
func cutRunes(v string, n int) string {
	if r := []rune(v); len(r) > n {
		return string(r[:n]) + " [cut]"
	}
	return v
}

// telemetryInstructions are appended to the instructions while the
// telemetry tools exist.
const telemetryInstructions = `

Telemetry: when it is on (telemetry_status), these tools read what shed collects. Every record carries the same labels. VictoriaMetrics: shed_project_id, shed_project_name, shed_service_id, shed_service_name, shed_service_kind, shed_deployment_id, shed_source (otlp, container, edge, exporter, sampler, docker, egress, audit, shed). VictoriaLogs has the same as fields with dots (shed.service.id, shed.project.id, shed.source, shed.deployment.id) and stream fields shed.project.id, shed.service.id, shed.source; container records also have stream (stdout or stderr), level, and trace_id. shed itself is service ID "shed". Filter by IDs, which survive renames.
- query_metrics samples: sum(shed_container_memory_bytes{shed_service_id="ID"}); avg_over_time(shed_container_cpu_percent{shed_service_id="ID"}[5m]); sum by (code) (rate(shed_http_requests_total{shed_service_id="ID"}[5m])); histogram_quantile(0.95, sum by (le) (rate(shed_http_request_duration_seconds_bucket{shed_service_id="ID"}[5m]))); increase(shed_container_restarts_total{shed_service_id="ID"}[1h]); shed_telemetry_dropped_total. Container series: shed_container_{cpu_percent,memory_bytes,network_{rx,tx}_bytes_per_second,disk_{read,write}_bytes_per_second,cpu_limit_percent,memory_limit_bytes}; host series: shed_host_*. NaN and infinite values come back as null.
- query_logs samples: _stream:{"shed.service.id"="ID"} error; _stream:{"shed.service.id"="ID","shed.source"="edge"} status:>=500 | limit 20; _stream:{"shed.source"="audit"} | limit 50. Edge records have client_ip, method, host, path, status, duration (seconds), and trace_id.
- A trace_id on a log record, a slow request, or a deployment opens in get_trace. error_logs, slow_requests, and top_queries answer common questions directly.
- Telemetry tools fail with "telemetry is off" while it is off; the operator turns it on under Server > Telemetry.`
