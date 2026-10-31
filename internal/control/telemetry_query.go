package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/grafana"
	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/victoria"
)

// MetricsStore answers MetricsQL queries. *victoria.Metrics implements it.
type MetricsStore interface {
	Query(ctx context.Context, q string, t time.Time) (*victoria.Result, error)
	QueryRange(ctx context.Context, q string, start, end time.Time, step time.Duration) (*victoria.Result, error)
}

// LogsStore answers LogsQL queries. *victoria.Logs implements it.
type LogsStore interface {
	Query(ctx context.Context, logsql string, start, end time.Time, limit int) ([]victoria.Record, error)
}

// TracesStore answers trace queries. *victoria.Traces implements it.
type TracesStore interface {
	SearchTraces(ctx context.Context, p victoria.SearchParams) ([]victoria.TraceSummary, error)
	Trace(ctx context.Context, id string) (*victoria.Trace, error)
}

// TelemetryStores are the stores and Grafana that telemetry queries read.
type TelemetryStores struct {
	Metrics MetricsStore
	Logs    LogsStore
	Traces  TracesStore
}

var (
	_ MetricsStore = (*victoria.Metrics)(nil)
	_ LogsStore    = (*victoria.Logs)(nil)
	_ TracesStore  = (*victoria.Traces)(nil)
)

// Limits on what telemetry queries return.
const (
	DefaultLogRecords = 200
	// MaxLogRecords is the most records a log query returns.
	MaxLogRecords = 2000

	DefaultHistoryRecords = 100
	// MaxHistoryRecords is the most records a page of log history holds.
	MaxHistoryRecords = 500

	DefaultTraces = 20
	// MaxTraces is the most traces a search returns.
	MaxTraces = 100

	// DefaultTopQueries is how many statements TopQueries returns.
	DefaultTopQueries = 20
	// MaxTopQueries is the most statements TopQueries returns.
	MaxTopQueries = 100

	// MaxTraceSpans is the most spans Trace returns.
	MaxTraceSpans = 1000
)

// requireTelemetry fails with a conflict while telemetry is off.
func (p *Plane) requireTelemetry(ctx context.Context) (observability.Settings, error) {
	s, err := p.telemetry.Settings(ctx)
	if err != nil {
		return s, err
	}
	if !s.Enabled {
		return s, observability.ErrDisabled
	}
	return s, nil
}

// storeError turns a failed store query into an error for the caller: a
// query the store rejected is invalid, anything else an upstream failure.
func (p *Plane) storeError(op string, err error) error {
	var se *victoria.StatusError
	if errors.As(err, &se) && se.Code >= 400 && se.Code < 500 {
		return errorf(ErrInvalid, "%s: %s", op, se.Body)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, victoria.ErrTooLarge) {
		return errorf(ErrInvalid, "%s: the answer is too large; narrow the query", op)
	}
	p.log.Warn("telemetry store query failed", "op", op, "err", err)
	return errorf(ErrUpstream, "%s: the store did not answer", op)
}

// MetricsQuery is a MetricsQL query. With a zero Start it is evaluated at
// Time, zero meaning now; otherwise at every Step from Start to End.
type MetricsQuery struct {
	Query      string
	Time       time.Time
	Start, End time.Time
	Step       time.Duration
}

// QueryMetrics evaluates a MetricsQL query.
func (p *Plane) QueryMetrics(ctx context.Context, q MetricsQuery) (*victoria.Result, error) {
	if strings.TrimSpace(q.Query) == "" {
		return nil, errorf(ErrInvalid, "query is required")
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	var res *victoria.Result
	var err error
	if q.Start.IsZero() {
		res, err = p.stores.Metrics.Query(ctx, q.Query, q.Time)
	} else {
		if q.Step <= 0 || !q.End.After(q.Start) {
			return nil, errorf(ErrInvalid, "a range query needs end after start and a positive step")
		}
		res, err = p.stores.Metrics.QueryRange(ctx, q.Query, q.Start, q.End, q.Step)
	}
	if err != nil {
		return nil, p.storeError("metrics query", err)
	}
	return res, nil
}

// QueryLogs runs a LogsQL query over [start, end), zero leaving a side open,
// and returns at most limit records (MaxLogRecords at most, DefaultLogRecords
// for a limit under 1), newest first, with the saved values of every
// service's variables masked in every field.
func (p *Plane) QueryLogs(ctx context.Context, logsql string, start, end time.Time, limit int) ([]victoria.Record, error) {
	if strings.TrimSpace(logsql) == "" {
		return nil, errorf(ErrInvalid, "query is required")
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	return p.queryLogs(ctx, logsql, start, end, clampLimit(limit, DefaultLogRecords, MaxLogRecords))
}

// queryLogs runs a LogsQL query and masks the records.
func (p *Plane) queryLogs(ctx context.Context, logsql string, start, end time.Time, limit int) ([]victoria.Record, error) {
	records, err := p.stores.Logs.Query(ctx, logsql, start, end, limit)
	if err != nil {
		return nil, p.storeError("logs query", err)
	}
	mask, err := p.ShedMask(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		for k, v := range r {
			r[k] = mask(v)
		}
	}
	return records, nil
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	return min(n, max)
}

// A LogRecord is one stored log record of a service.
type LogRecord struct {
	Time time.Time
	// Source is "container" or "otlp".
	Source string
	// Stream is "stdout" or "stderr" for container records, else "".
	Stream       string
	Level        string
	Message      string
	DeploymentID string
	TraceID      string
	// Fields are the record's other fields.
	Fields map[string]string
}

// LogQuery selects the stored logs of a service.
type LogQuery struct {
	// Search is the text of the search box; see [victoria.SearchFilter].
	Search string
	// Level is "", debug, info, warn, or error.
	Level string
	// From and To bound the records' time; a zero side is open.
	From, To time.Time
	// Before is the cursor of the previous page: only older records are
	// returned.
	Before time.Time
	// Limit is the page size, DefaultHistoryRecords for under 1 and at most
	// MaxHistoryRecords.
	Limit int
}

// LogPage is a page of log records, newest first.
type LogPage struct {
	Records []LogRecord
	// Next is the cursor of the next, older page: the time of this page's
	// oldest record. It is empty after the last page.
	Next string
}

// levelNames maps a level of the log search to the values records carry.
var levelNames = map[string][]string{
	"debug": {"debug", "trace"},
	"info":  {"info"},
	"warn":  {"warn", "warning"},
	"error": {"error", "err", "fatal", "critical"},
}

// levelFields are the fields that may hold a record's level.
// severity_text is where VictoriaLogs keeps an OTLP record's.
var levelFields = []string{"level", "severity", "lvl", "severity_text"}

// severityNumbers are the OTLP severity numbers of each level, inclusive:
// TRACE through DEBUG4, INFO through INFO4, WARN through WARN4, and ERROR
// through FATAL4. An SDK need not set severity_text.
var severityNumbers = map[string][2]int{
	"debug": {1, 8},
	"info":  {9, 12},
	"warn":  {13, 16},
	"error": {17, 24},
}

// levelFilter returns the filter that matches records of a level, ""
// meaning any.
func levelFilter(level string) (string, error) {
	if level == "" {
		return "", nil
	}
	names, ok := levelNames[level]
	if !ok {
		return "", errorf(ErrInvalid, "level must be debug, info, warn, or error")
	}
	var terms []string
	for _, f := range levelFields {
		for _, n := range names {
			terms = append(terms, victoria.Quote(f)+":i("+victoria.Quote(n)+")")
		}
	}
	r := severityNumbers[level]
	terms = append(terms, fmt.Sprintf(`"severity_number":range[%d, %d]`, r[0], r[1]))
	return "(" + strings.Join(terms, " OR ") + ")", nil
}

// serviceLogFilter returns the LogsQL filter for the records of a service
// that match search and level. The service's stream filter always comes
// first and the rest only narrows it.
func serviceLogFilter(serviceID, search, level string) (string, error) {
	filters := []string{victoria.StreamFilter("shed.service.id", serviceID)}
	f, err := victoria.SearchFilter(search)
	if err != nil {
		return "", errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), victoria.ErrSearch.Error()+": "))
	}
	if f != "" {
		filters = append(filters, f)
	}
	lf, err := levelFilter(level)
	if err != nil {
		return "", err
	}
	if lf != "" {
		filters = append(filters, lf)
	}
	return strings.Join(filters, " AND "), nil
}

// LogHistory searches the stored logs of a service across deployments,
// newest first.
func (p *Plane) LogHistory(ctx context.Context, serviceID string, q LogQuery) (LogPage, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return LogPage{}, err
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return LogPage{}, err
	}
	filter, err := serviceLogFilter(svc.ID, q.Search, q.Level)
	if err != nil {
		return LogPage{}, err
	}
	end := q.To
	if !q.Before.IsZero() {
		// The cursor is the time of a record already returned.
		if before := q.Before.Add(-time.Nanosecond); end.IsZero() || before.Before(end) {
			end = before
		}
	}
	limit := clampLimit(q.Limit, DefaultHistoryRecords, MaxHistoryRecords)
	records, err := p.queryLogs(ctx, filter, q.From, end, limit)
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{Records: make([]LogRecord, 0, len(records))}
	for _, r := range records {
		page.Records = append(page.Records, toLogRecord(r))
	}
	if len(page.Records) == limit {
		page.Next = page.Records[len(page.Records)-1].Time.Format(time.RFC3339Nano)
	}
	return page, nil
}

// consumedFields are the stored fields that LogRecord has a place for.
var consumedFields = map[string]bool{
	"_time": true, "_msg": true, "_stream": true, "_stream_id": true,
	"shed.source": true, "shed.deployment.id": true, "shed.service.id": true, "shed.project.id": true,
	"stream": true, "trace_id": true,
}

func toLogRecord(r victoria.Record) LogRecord {
	t, _ := time.Parse(time.RFC3339Nano, r["_time"])
	rec := LogRecord{
		Time: t.UTC(), Source: r["shed.source"], Stream: r["stream"], Message: r["_msg"],
		DeploymentID: r["shed.deployment.id"], TraceID: r["trace_id"], Fields: map[string]string{},
	}
	for _, f := range levelFields {
		if v := r[f]; v != "" {
			rec.Level = strings.ToLower(v)
			break
		}
	}
	if n, err := strconv.Atoi(r["severity_number"]); rec.Level == "" && err == nil {
		for level, rng := range severityNumbers {
			if rng[0] <= n && n <= rng[1] {
				rec.Level = level
			}
		}
	}
	for k, v := range r {
		if !consumedFields[k] && !slices.Contains(levelFields, k) {
			rec.Fields[k] = v
		}
	}
	return rec
}

// ErrorLogs returns the error-level log records of a service since a time,
// newest first, masked like [Plane.QueryLogs].
func (p *Plane) ErrorLogs(ctx context.Context, serviceID string, since time.Time, limit int) ([]victoria.Record, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	filter, err := serviceLogFilter(svc.ID, "", "error")
	if err != nil {
		return nil, err
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	return p.queryLogs(ctx, filter, since, time.Time{}, clampLimit(limit, DefaultLogRecords, MaxLogRecords))
}

// A SlowRequest is one request the edge recorded.
type SlowRequest struct {
	Time               time.Time
	Method, Host, Path string
	Status             int
	DurationMs         float64
	ClientIP, TraceID  string
}

// SlowRequests holds the latency percentiles of a service's requests and its
// slowest ones.
type SlowRequests struct {
	P50Ms, P95Ms, P99Ms float64
	Requests            []SlowRequest
}

// SlowRequests returns the percentiles of the request duration of a service
// since a time, and its limit slowest requests (DefaultTraces for under 1,
// MaxTraces at most), from the edge access records.
func (p *Plane) SlowRequests(ctx context.Context, serviceID string, since time.Time, limit int) (SlowRequests, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return SlowRequests{}, err
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return SlowRequests{}, err
	}
	limit = clampLimit(limit, DefaultTraces, MaxTraces)
	edge := victoria.StreamFilter("shed.service.id", svc.ID, "shed.source", "edge")
	var out SlowRequests

	stats, err := p.stores.Logs.Query(ctx, edge+
		` | stats quantile(0.5, duration) p50, quantile(0.95, duration) p95, quantile(0.99, duration) p99`, since, time.Time{}, 0)
	if err != nil {
		return SlowRequests{}, p.storeError("logs query", err)
	}
	if len(stats) > 0 {
		out.P50Ms, out.P95Ms, out.P99Ms = seconds(stats[0]["p50"]), seconds(stats[0]["p95"]), seconds(stats[0]["p99"])
	}
	records, err := p.queryLogs(ctx, edge+` | sort by (duration desc) | limit `+strconv.Itoa(limit), since, time.Time{}, 0)
	if err != nil {
		return SlowRequests{}, err
	}
	out.Requests = make([]SlowRequest, 0, len(records))
	for _, r := range records {
		t, _ := time.Parse(time.RFC3339Nano, r["_time"])
		status, _ := strconv.Atoi(r["status"])
		out.Requests = append(out.Requests, SlowRequest{
			Time: t.UTC(), Method: r["method"], Host: r["host"], Path: r["path"], Status: status,
			DurationMs: seconds(r["duration"]), ClientIP: r["client_ip"], TraceID: r["trace_id"],
		})
	}
	return out, nil
}

// seconds converts a duration in seconds, as text, to milliseconds.
func seconds(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v * 1000
}

// A QueryStat is the statistics of one statement of a database.
type QueryStat struct {
	Query   string
	Calls   float64
	TotalMs float64
	MeanMs  float64
	Rows    float64
}

// statementMetrics are the exporter series of the statement statistics of a
// database kind: the call count, total seconds, and row count counters, and
// the label holding the statement's text. postgres_exporter keys its
// counters by queryid and reports the text only on a separate series, text,
// which the counters are joined with.
var statementMetrics = map[string]struct{ calls, seconds, rows, label, key, text string }{
	"postgres": {
		calls: "pg_stat_statements_calls_total", seconds: "pg_stat_statements_seconds_total", rows: "pg_stat_statements_rows_total",
		label: "query", key: "queryid", text: "pg_stat_statements_query_id",
	},
	"mysql": {
		calls: "mysql_perf_schema_events_statements_total", seconds: "mysql_perf_schema_events_statements_seconds_total",
		rows: "mysql_perf_schema_events_statements_rows_sent_total", label: "digest_text",
	},
}

// TopQueries returns the statements of a Postgres or MySQL service that took
// the most time in total (at most limit; DefaultTopQueries for under 1,
// MaxTopQueries at most), from the statistics its exporter reports.
func (p *Plane) TopQueries(ctx context.Context, serviceID string, limit int) ([]QueryStat, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	m, ok := statementMetrics[svc.Kind]
	if !ok {
		return nil, errorf(ErrInvalid, "top_queries covers postgres and mysql services, not %s", svc.Kind)
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	limit = clampLimit(limit, DefaultTopQueries, MaxTopQueries)
	query := func(metric string) (map[string]float64, error) {
		sel := "{shed_service_id=" + strconv.Quote(svc.ID) + "}"
		q := fmt.Sprintf("sum by (%s) (%s%s)", m.label, metric, sel)
		if m.key != "" {
			q = fmt.Sprintf("sum by (%[1]s) (sum by (%[2]s) (%[3]s%[4]s) * on (%[2]s) group_left (%[1]s) max by (%[2]s, %[1]s) (%[5]s%[4]s))",
				m.label, m.key, metric, sel, m.text)
		}
		res, err := p.stores.Metrics.Query(ctx, q, time.Time{})
		if err != nil {
			return nil, p.storeError("metrics query", err)
		}
		out := make(map[string]float64, len(res.Series))
		for _, s := range res.Series {
			if len(s.Values) > 0 && s.Labels[m.label] != "" {
				out[s.Labels[m.label]] = s.Values[0].Value
			}
		}
		return out, nil
	}
	totals, err := query(m.seconds)
	if err != nil {
		return nil, err
	}
	calls, err := query(m.calls)
	if err != nil {
		return nil, err
	}
	rows, err := query(m.rows)
	if err != nil {
		return nil, err
	}
	stats := make([]QueryStat, 0, len(totals))
	for text, secs := range totals {
		s := QueryStat{Query: text, Calls: calls[text], TotalMs: secs * 1000, Rows: rows[text]}
		if s.Calls > 0 {
			s.MeanMs = s.TotalMs / s.Calls
		}
		stats = append(stats, s)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].TotalMs != stats[j].TotalMs {
			return stats[i].TotalMs > stats[j].TotalMs
		}
		return stats[i].Query < stats[j].Query
	})
	if len(stats) > limit {
		stats = stats[:limit]
	}
	return stats, nil
}

// SearchTraces finds traces, only those of the service serviceID when it is
// not empty.
func (p *Plane) SearchTraces(ctx context.Context, serviceID string, sp victoria.SearchParams) ([]victoria.TraceSummary, error) {
	if serviceID != "" {
		svc, err := p.store.Service(ctx, serviceID)
		if err != nil {
			return nil, err
		}
		tags := maps.Clone(sp.Tags)
		if tags == nil {
			tags = map[string]string{}
		}
		tags["resource_attr:shed.service.id"] = svc.ID
		sp.Tags = tags
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	sp.Limit = clampLimit(sp.Limit, DefaultTraces, MaxTraces)
	traces, err := p.stores.Traces.SearchTraces(ctx, sp)
	if err != nil {
		return nil, p.storeError("trace search", err)
	}
	return traces, nil
}

// Trace returns the spans of a trace.
func (p *Plane) Trace(ctx context.Context, traceID string) (*victoria.Trace, error) {
	if !ValidTraceID(traceID) {
		return nil, errorf(ErrInvalid, "trace_id must be 32 hexadecimal characters")
	}
	if _, err := p.requireTelemetry(ctx); err != nil {
		return nil, err
	}
	tr, err := p.stores.Traces.Trace(ctx, traceID)
	if errors.Is(err, victoria.ErrTraceNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, p.storeError("trace lookup", err)
	}
	return tr, nil
}

var traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ValidTraceID reports whether s is a trace ID: 32 lowercase hexadecimal
// characters.
func ValidTraceID(s string) bool { return traceIDPattern.MatchString(s) }

// Dashboards lists Grafana's dashboards, only those of folder folderUID when
// it is not empty.
func (p *Plane) Dashboards(ctx context.Context, folderUID string) ([]observability.Dashboard, error) {
	return p.dashboards.List(ctx, folderUID)
}

// TraceExploreURL returns the Grafana Explore URL, relative to the server,
// that shows a trace from the shed-traces data source. It fails while
// telemetry is off and for something that is not a trace ID.
func (p *Plane) TraceExploreURL(ctx context.Context, traceID string) (string, error) {
	if !ValidTraceID(traceID) {
		return "", errorf(ErrInvalid, "trace ID must be 32 hexadecimal characters")
	}
	s, err := p.requireTelemetry(ctx)
	if err != nil {
		return "", err
	}
	return ExploreTraceURL(traceID, s.TracesRetentionDays), nil
}

// ExploreTraceURL builds the Explore URL of a trace for Grafana 13: the
// panes parameter holds the state of one pane, keyed by an arbitrary ID, and
// schemaVersion 1 says so. The Jaeger data source reads a trace ID from the
// query's query field. The range reaches back as far as traces are kept.
func ExploreTraceURL(traceID string, retentionDays int) string {
	ds := map[string]string{"type": "jaeger", "uid": grafana.TracesUID}
	panes := map[string]any{"trc": map[string]any{
		"datasource": grafana.TracesUID,
		"queries":    []any{map[string]any{"refId": "A", "datasource": ds, "query": traceID}},
		"range":      map[string]string{"from": "now-" + strconv.Itoa(max(retentionDays, 1)) + "d", "to": "now"},
	}}
	b, _ := json.Marshal(panes)
	v := url.Values{"schemaVersion": {"1"}, "panes": {string(b)}, "orgId": {"1"}}
	return "/grafana/explore?" + v.Encode()
}
