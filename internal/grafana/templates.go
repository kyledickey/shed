package grafana

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Dashboard is a dashboard rendered from a template for one scope.
type Dashboard struct {
	// UID is "<template>-<scope ID>", or the template name for shed's own
	// dashboards.
	UID      string
	Template string
	Title    string
	// Version is the template's version.
	Version int
	// JSON is the dashboard model for [Client.PutDashboard].
	JSON json.RawMessage
}

// template builds one kind of dashboard. Bump version whenever the output
// changes, so reconciliation overwrites dashboards nobody customized.
type template struct {
	name    string
	title   string
	version int
	build   func(b *board, scopeID string)
}

// ServiceDashboards renders the dashboards of a service of kind "app" or a
// database kind.
func ServiceDashboards(serviceID, kind string) []Dashboard {
	ts := []template{
		{"svc-overview", "Overview", 1, serviceOverview},
		{"svc-logs", "Logs", 1, serviceLogs},
	}
	switch kind {
	case "app":
		ts = append(ts,
			template{"svc-http", "HTTP", 1, appHTTP},
			template{"svc-traces", "Traces", 1, appTraces})
	case "postgres":
		ts = append(ts, template{"svc-postgres", "Postgres", 1, postgres})
	case "mysql":
		ts = append(ts, template{"svc-mysql", "MySQL", 1, mysql})
	case "mongo":
		ts = append(ts, template{"svc-mongo", "MongoDB", 1, mongo})
	case "redis":
		ts = append(ts, template{"svc-redis", "Redis", 1, redis})
	}
	return render(ts, serviceID, []string{"shed", "service:" + serviceID})
}

// ProjectDashboards renders the dashboards of a project.
func ProjectDashboards(projectID string) []Dashboard {
	ts := []template{{"proj-overview", "Overview", 1, projectOverview}}
	return render(ts, projectID, []string{"shed", "project:" + projectID})
}

// ShedDashboards renders the dashboards of shed itself.
func ShedDashboards() []Dashboard {
	ts := []template{
		{"shed-overview", "Overview", 1, shedOverview},
		{"shed-deploys", "Deploys", 1, shedDeploys},
		{"shed-api", "API & MCP", 1, shedAPI},
		{"shed-proxy", "Proxy", 1, shedProxy},
		{"shed-host", "Host & Docker", 1, shedHost},
		{"shed-backups", "Backups", 1, shedBackups},
		{"shed-runtime", "Runtime", 1, shedRuntime},
		{"shed-telemetry", "Telemetry", 1, shedTelemetry},
		{"shed-security", "Security", 1, shedSecurity},
	}
	return render(ts, "", []string{"shed"})
}

func render(ts []template, scopeID string, tags []string) []Dashboard {
	out := make([]Dashboard, len(ts))
	for i, t := range ts {
		uid := t.name
		if scopeID != "" {
			uid += "-" + scopeID
		}
		b := newBoard(uid, t.title, tags, tags[len(tags)-1:])
		t.build(b, scopeID)
		js, err := json.Marshal(b.d)
		if err != nil {
			panic(fmt.Sprintf("grafana: encode template %s: %v", t.name, err))
		}
		out[i] = Dashboard{UID: uid, Template: t.name, Title: t.title, Version: t.version, JSON: js}
	}
	return out
}

// sel returns a metric selector: name{matchers...}.
func sel(name string, matchers ...string) string {
	return name + "{" + strings.Join(matchers, ",") + "}"
}

// stream returns a LogsQL stream filter: {field="value", ...}.
func stream(filters ...string) string {
	return "{" + strings.Join(filters, ", ") + "}"
}

func eq(label, value string) string { return fmt.Sprintf("%s=%q", label, value) }

const (
	// errorLevels matches error records of container logs (level) and
	// OTLP logs (severity).
	errorLevels = `(level:~"(?i)^(err|error|fatal|panic|crit|critical)$" or severity:~"(?i)^(error|fatal)")`

	// appLogs are the stream fields of a service's own log records.
	appLogs = `shed.source in ("container", "otlp")`
)

func rate(s string) string { return rateOver(s, "$__rate_interval") }

func rateOver(s, window string) string { return "rate(" + s + "[" + window + "])" }

func increase(s, window string) string { return "increase(" + s + "[" + window + "])" }

// quantile is the q-quantile of a histogram, aggregated by the labels in by.
func quantile(q float64, by, bucket string) string {
	return fmt.Sprintf("histogram_quantile(%g, sum by (%s) (%s))", q, by, rate(bucket))
}

// statusClass sums a request counter by status class (2xx, 4xx, ...).
func statusClass(selector string) string {
	return fmt.Sprintf(`sum by (class) (label_replace(%s, "class", "${1}xx", "code", "([1-5]).."))`, rate(selector))
}

// ratio5xx is the share of edge requests matching matchers that were
// answered with 5xx, over window.
func ratio5xx(by, window string, matchers ...string) string {
	all := sel("shed_http_requests_total", matchers...)
	errs := sel("shed_http_requests_total", append(slices.Clone(matchers), `code=~"5.."`)...)
	return fmt.Sprintf("sum %s (%s) / sum %s (%s)", by, rateOver(errs, window), by, rateOver(all, window))
}

func serviceOverview(b *board, id string) {
	s := eq("shed_service_id", id)
	b.add(6, 4, stat("Status", "", metricNow(fmt.Sprintf("max(present_over_time(%s[2m]))", sel("shed_container_memory_bytes", s)), ""), "Not running").
		mapValue("1", "Running", "green"))
	b.add(6, 4, stat("Restarts", "short", metricNow(fmt.Sprintf("sum(%s)", increase(sel("shed_container_restarts_total", s), "$__range")), ""), "0"))
	b.add(6, 4, stat("OOM kills", "short", metricNow(fmt.Sprintf("sum(%s)", increase(sel("shed_container_oom_kills_total", s), "$__range")), ""), "0"))
	b.add(6, 4, stat("Memory now", "bytes", metricNow(fmt.Sprintf("sum(%s)", sel("shed_container_memory_bytes", s)), ""), "-"))

	b.add(12, 8, timeseries("CPU", "percent",
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_cpu_percent", s)), "used"),
		metric(fmt.Sprintf("max(%s > 0)", sel("shed_container_cpu_limit_percent", s)), "limit"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Memory", "bytes",
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_memory_bytes", s)), "used"),
		metric(fmt.Sprintf("max(%s > 0)", sel("shed_container_memory_limit_bytes", s)), "limit"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Network", "Bps",
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_network_rx_bytes_per_second", s)), "received"),
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_network_tx_bytes_per_second", s)), "sent"),
	))
	b.add(12, 8, timeseries("Block I/O", "Bps",
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_disk_read_bytes_per_second", s)), "read"),
		metric(fmt.Sprintf("sum(%s)", sel("shed_container_disk_write_bytes_per_second", s)), "written"),
	))
}

func serviceLogs(b *board, id string) {
	logs := stream(eq("shed.service.id", id), appLogs)
	b.textVar("search", "Search (LogsQL)", "*")
	b.add(16, 8, bars("Volume by level", "short",
		logSeries(logs+" | stats by (level, severity) count() logs", "{{level}}{{severity}}")))
	b.add(8, 8, bars("Errors", "short",
		logSeries(logs+" "+errorLevels+" | stats count() errors", "errors")))
	b.add(24, 16, logsPanel("Logs", logLines(logs+" $search")))
}

func appHTTP(b *board, id string) {
	s := eq("shed_service_id", id)
	b.metricVar("host", "host", sel("shed_http_requests_total", s), true)
	host := `host=~"$host"`
	reqs := sel("shed_http_requests_total", s, host)
	buckets := sel("shed_http_request_duration_seconds_bucket", s, host)
	edge := stream(eq("shed.service.id", id), eq("shed.source", "edge")) + ` host:~"^$host$"`

	b.add(12, 8, timeseries("Requests/s by status", "reqps", metric(statusClass(reqs), "{{class}}")))
	b.add(12, 8, timeseries("5xx ratio", "percentunit", metric(ratio5xx("", "$__rate_interval", s, host), "5xx")))
	b.add(12, 8, timeseries("Latency", "s",
		metric(quantile(0.5, "le", buckets), "p50"),
		metric(quantile(0.95, "le", buckets), "p95"),
		metric(quantile(0.99, "le", buckets), "p99"),
	))
	b.add(12, 8, timeseries("Response bytes/s", "Bps",
		metric(fmt.Sprintf("sum(%s)", rate(sel("shed_http_response_bytes_total", s, host))), "sent")))
	b.add(12, 10, top("Top paths", "short",
		logTotals(edge+" | stats by (path) count() requests | sort by (requests desc) | limit 10", "{{path}}")))
	b.add(12, 10, top("Top client IPs", "short",
		logTotals(edge+" | stats by (client_ip) count() requests | sort by (requests desc) | limit 10", "{{client_ip}}")))
}

func appTraces(b *board, id string) {
	s := eq("shed_service_id", id)
	count := sel("shed_span_duration_seconds_count", s)
	b.metricVar("service_name", "service_name", count, false)

	b.add(12, 8, timeseries("Span rate by name", "ops",
		metric(fmt.Sprintf("topk(10, sum by (span_name) (%s))", rate(count)), "{{span_name}}")))
	b.add(12, 8, timeseries("Errors by name", "ops",
		metric(fmt.Sprintf("topk(10, sum by (span_name) (%s))", rate(sel("shed_span_duration_seconds_count", s, `status_code="error"`))), "{{span_name}}")))
	b.add(24, 8, timeseries("p95 duration by name", "s",
		metric(fmt.Sprintf("topk(10, %s)", quantile(0.95, "span_name, le", sel("shed_span_duration_seconds_bucket", s))), "{{span_name}}")))
	b.add(24, 10, table("Slowest recent traces",
		traceSearch("$service_name", "resource_attr:shed.service.id="+id, 50),
	).transform("sortBy", map[string]any{"sort": []map[string]any{{"field": "Duration", "desc": true}}}))
}

// slowLog adds a logs panel of a database's slow-query lines.
func slowLog(b *board, id, title, filter string) {
	b.add(24, 10, logsPanel(title, logLines(stream(eq("shed.service.id", id), eq("shed.source", "container"))+" "+filter)))
}

func postgres(b *board, id string) {
	s := eq("shed_service_id", id)
	sum := func(name string) string { return fmt.Sprintf("sum(%s)", rate(sel(name, s))) }

	b.add(12, 8, timeseries("Connections by state", "short",
		metric(fmt.Sprintf("sum by (state) (%s)", sel("pg_stat_activity_count", s)), "{{state}}"),
		metric(fmt.Sprintf("max(%s)", sel("pg_settings_max_connections", s)), "max_connections"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Commits and rollbacks/s", "ops",
		metric(sum("pg_stat_database_xact_commit"), "commits"),
		metric(sum("pg_stat_database_xact_rollback"), "rollbacks"),
	))
	b.add(8, 8, timeseries("Cache hit ratio", "percentunit",
		metric(fmt.Sprintf("%s / (%s + %s)", sum("pg_stat_database_blks_hit"), sum("pg_stat_database_blks_hit"), sum("pg_stat_database_blks_read")), "hit ratio")))
	b.add(16, 8, timeseries("Row operations/s", "ops",
		metric(sum("pg_stat_database_tup_fetched"), "fetched"),
		metric(sum("pg_stat_database_tup_inserted"), "inserted"),
		metric(sum("pg_stat_database_tup_updated"), "updated"),
		metric(sum("pg_stat_database_tup_deleted"), "deleted"),
	))
	b.add(8, 8, timeseries("Locks by mode", "short",
		metric(fmt.Sprintf("sum by (mode) (%s)", sel("pg_locks_count", s)), "{{mode}}")))
	b.add(8, 8, bars("Deadlocks", "short",
		metric(fmt.Sprintf("sum(%s)", increase(sel("pg_stat_database_deadlocks", s), "$__rate_interval")), "deadlocks")))
	b.add(8, 8, timeseries("Size", "bytes",
		metric(fmt.Sprintf("sum by (datname) (%s)", sel("pg_database_size_bytes", s)), "{{datname}}"),
		metric(fmt.Sprintf("max(%s)", sel("pg_wal_size_bytes", s)), "WAL"),
	))
	b.add(24, 10, table("Top statements by total time",
		metricNow(fmt.Sprintf("topk(10, sum by (queryid) (%s) * on (queryid) group_left (query) max by (queryid, query) (%s))",
			increase(sel("pg_stat_statements_seconds_total", s), "$__range"), sel("pg_stat_statements_query_id", s)), ""),
	).unitOf("Value", "s").transform("organize", map[string]any{
		"excludeByName": map[string]bool{"Time": true},
		"renameByName":  map[string]string{"Value": "total time"},
	}))
	slowLog(b, id, "Slow queries", `~"duration: [0-9.]+ ms"`)
}

func mysql(b *board, id string) {
	s := eq("shed_service_id", id)
	sum := func(name string) string { return fmt.Sprintf("sum(%s)", rate(sel(name, s))) }

	b.add(12, 8, timeseries("Connections", "short",
		metric(fmt.Sprintf("max(%s)", sel("mysql_global_status_threads_connected", s)), "connected"),
		metric(fmt.Sprintf("max(%s)", sel("mysql_global_variables_max_connections", s)), "max_connections"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Queries/s by command", "ops",
		metric(fmt.Sprintf("topk(10, sum by (command) (%s))", rate(sel("mysql_global_status_commands_total", s))), "{{command}}")))
	b.add(8, 8, timeseries("InnoDB buffer pool hit ratio", "percentunit",
		metric(fmt.Sprintf("1 - %s / %s", sum("mysql_global_status_innodb_buffer_pool_reads"), sum("mysql_global_status_innodb_buffer_pool_read_requests")), "hit ratio")))
	b.add(8, 8, timeseries("Row operations/s", "ops",
		metric(fmt.Sprintf("sum by (operation) (%s)", rate(sel("mysql_global_status_innodb_row_ops_total", s))), "{{operation}}")))
	b.add(8, 8, timeseries("Lock waits/s", "ops",
		metric(sum("mysql_global_status_innodb_row_lock_waits"), "row locks"),
		metric(sum("mysql_global_status_table_locks_waited"), "table locks"),
	))
	b.add(24, 10, table("Top digests by total time",
		metricNow(fmt.Sprintf("topk(10, sum by (schema, digest_text) (%s))", increase(sel("mysql_perf_schema_events_statements_seconds_total", s), "$__range")), ""),
	).unitOf("Value", "s").transform("organize", map[string]any{
		"excludeByName": map[string]bool{"Time": true},
		"renameByName":  map[string]string{"Value": "total time"},
	}))
	slowLog(b, id, "Slow query log", `"Query_time"`)
}

func mongo(b *board, id string) {
	s := eq("shed_service_id", id)

	b.add(12, 8, timeseries("Operations/s by type", "ops",
		metric(fmt.Sprintf("sum by (legacy_op_type) (%s)", rate(sel("mongodb_ss_opcounters", s))), "{{legacy_op_type}}")))
	b.add(12, 8, timeseries("Connections", "short",
		metric(fmt.Sprintf("max(%s)", sel("mongodb_ss_connections", s, `conn_type="current"`)), "current"),
		metric(fmt.Sprintf("max(%s)", sel("mongodb_ss_connections", s, `conn_type="available"`)), "available"),
	))
	b.add(12, 8, timeseries("WiredTiger cache", "bytes",
		metric(fmt.Sprintf("max(%s)", sel("mongodb_ss_wt_cache_bytes_currently_in_the_cache", s)), "in cache"),
		metric(fmt.Sprintf("max(%s)", sel("mongodb_ss_wt_cache_maximum_bytes_configured", s)), "maximum"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Latency by operation", "µs",
		metric(fmt.Sprintf("sum by (op_type) (%s) / sum by (op_type) (%s)",
			rate(sel("mongodb_ss_opLatencies_latency", s)), rate(sel("mongodb_ss_opLatencies_ops", s))), "{{op_type}}")))
	b.add(24, 8, bars("Slow operations", "short",
		metric(fmt.Sprintf("sum by (database) (%s)", sel("mongodb_profile_slow_query_count", s)), "{{database}}")))
	slowLog(b, id, "Slow operation log", `"Slow query"`)
}

func redis(b *board, id string) {
	s := eq("shed_service_id", id)
	sum := func(name string) string { return fmt.Sprintf("sum(%s)", rate(sel(name, s))) }

	b.add(12, 8, timeseries("Commands/s", "ops", metric(sum("redis_commands_processed_total"), "commands")))
	b.add(12, 8, timeseries("Hit ratio", "percentunit",
		metric(fmt.Sprintf("%s / (%s + %s)", sum("redis_keyspace_hits_total"), sum("redis_keyspace_hits_total"), sum("redis_keyspace_misses_total")), "hit ratio")))
	b.add(12, 8, timeseries("Memory", "bytes",
		metric(fmt.Sprintf("max(%s)", sel("redis_memory_used_bytes", s)), "used"),
		metric(fmt.Sprintf("max(%s > 0)", sel("redis_memory_max_bytes", s)), "maxmemory"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Evictions/s", "ops", metric(sum("redis_evicted_keys_total"), "evicted")))
	b.add(8, 8, timeseries("Clients", "short",
		metric(fmt.Sprintf("max(%s)", sel("redis_connected_clients", s)), "connected"),
		metric(fmt.Sprintf("max(%s)", sel("redis_blocked_clients", s)), "blocked"),
	))
	b.add(8, 8, timeseries("Keys per database", "short",
		metric(fmt.Sprintf("sum by (db) (%s)", sel("redis_db_keys", s)), "{{db}}")))
	b.add(8, 8, timeseries("Slowlog", "short",
		metric(fmt.Sprintf("max(%s)", sel("redis_slowlog_length", s)), "entries"),
		metric(fmt.Sprintf("max(%s)", sel("redis_last_slow_execution_duration_seconds", s)), "last slow (s)"),
	))
}

func projectOverview(b *board, id string) {
	s := eq("shed_project_id", id)
	by := "by (shed_service_name)"
	reqs := sel("shed_http_requests_total", s)
	b.add(24, 10, table("Services",
		metricNow(fmt.Sprintf("max %s (present_over_time(%s[2m]))", by, sel("shed_container_memory_bytes", s)), ""),
		metricNow(fmt.Sprintf("sum %s (%s)", by, sel("shed_container_cpu_percent", s)), ""),
		metricNow(fmt.Sprintf("sum %s (%s)", by, sel("shed_container_memory_bytes", s)), ""),
		metricNow(fmt.Sprintf("sum %s (rate(%s[5m]))", by, reqs), ""),
		metricNow(ratio5xx(by, "5m", s), ""),
		logTotals(stream(eq("shed.project.id", id), appLogs)+" "+errorLevels+
			" | rename shed.service.name as shed_service_name | stats by (shed_service_name) count() errors", ""),
	).
		unitOf("Value #B", "percent").unitOf("Value #C", "bytes").unitOf("Value #D", "reqps").unitOf("Value #E", "percentunit").
		transform("labelsToFields", map[string]any{}).
		transform("joinByField", map[string]any{"byField": "shed_service_name", "mode": "outer"}).
		transform("organize", map[string]any{
			"excludeByName": map[string]bool{"Time": true, "Time 1": true, "Time 2": true, "Time 3": true, "Time 4": true, "Time 5": true, "Time 6": true},
			"renameByName": map[string]string{
				"shed_service_name": "service", "Value #A": "running", "Value #B": "CPU", "Value #C": "memory",
				"Value #D": "requests/s", "Value #E": "5xx ratio", "errors": "error logs",
			},
		}))
	b.add(12, 8, timeseries("CPU by service", "percent",
		metric(fmt.Sprintf("sum %s (%s)", by, sel("shed_container_cpu_percent", s)), "{{shed_service_name}}")))
	b.add(12, 8, timeseries("Memory by service", "bytes",
		metric(fmt.Sprintf("sum %s (%s)", by, sel("shed_container_memory_bytes", s)), "{{shed_service_name}}")))
	b.add(12, 8, timeseries("Requests/s by service", "reqps",
		metric(fmt.Sprintf("sum %s (%s)", by, rate(reqs)), "{{shed_service_name}}")))
	b.add(12, 8, bars("Error logs by service", "short",
		logSeries(stream(eq("shed.project.id", id), appLogs)+" "+errorLevels+" | stats by (shed.service.name) count() errors", "{{shed.service.name}}")))
}

const (
	// shedSelf matches shed's own metrics about itself.
	shedSelf = `shed_service_id="shed"`
	// shedRecorded matches every metric shed records itself, including
	// those about a service, whose shed_service_id names that service.
	shedRecorded = `shed_source="shed"`
	// shedLogs is the stream of shed's own log records.
	shedLogs = `{shed.service.id="shed", shed.source="shed"}`
)

func hostUsage(b *board) {
	b.add(12, 8, timeseries("Host CPU", "percent", metric("max(shed_host_cpu_percent)", "busy")))
	b.add(12, 8, timeseries("Host memory", "bytes",
		metric("max(shed_host_memory_bytes)", "used"),
		metric("max(shed_host_memory_total_bytes)", "total"),
	).limitLine("B"))
}

func shedOverview(b *board, _ string) {
	b.add(8, 4, stat("Host CPU", "percent", metricNow("max(shed_host_cpu_percent)", ""), "-"))
	b.add(8, 4, stat("Host memory", "percentunit", metricNow("max(shed_host_memory_bytes) / max(shed_host_memory_total_bytes)", ""), "-"))
	b.add(8, 4, stat("Host disk", "percentunit", metricNow("max(shed_host_disk_used_bytes) / max(shed_host_disk_total_bytes)", ""), "-"))
	b.add(12, 8, bars("Deployments by outcome", "short",
		metric(fmt.Sprintf("sum by (outcome) (%s)", increase(sel("shed_deployments_total", shedRecorded), "$__rate_interval")), "{{outcome}}")))
	b.add(12, 8, timeseries("Edge requests/s by status", "reqps", metric(statusClass("shed_http_requests_total"), "{{class}}")))
	hostUsage(b)
	b.add(24, 12, logsPanel("shed log", logLines(shedLogs)))
}

func shedDeploys(b *board, _ string) {
	b.add(12, 8, timeseries("Stage duration p95", "s",
		metric(quantile(0.95, "stage, le", sel("shed_deploy_stage_duration_seconds_bucket", shedRecorded)), "{{stage}}")))
	b.add(12, 8, bars("Outcomes", "short",
		metric(fmt.Sprintf("sum by (outcome) (%s)", increase(sel("shed_deployments_total", shedRecorded), "$__rate_interval")), "{{outcome}}")))
	b.add(8, 8, timeseries("Queue depth", "short", metric(fmt.Sprintf("max(%s)", sel("shed_deploy_queue_depth", shedSelf)), "queued")))
	b.add(8, 8, timeseries("Build cache hit ratio", "percentunit",
		metric(fmt.Sprintf("sum(%s) / sum(%s)", rate(sel("shed_build_steps_total", shedRecorded, `cached="true"`)), rate(sel("shed_build_steps_total", shedRecorded))), "cached")))
	b.add(8, 8, timeseries("Image pulls/s", "ops",
		metric(fmt.Sprintf("sum(%s)", rate(sel("shed_docker_request_duration_seconds_count", shedSelf, `op=~"(?i).*pull.*"`))), "pulls")))
}

func shedAPI(b *board, _ string) {
	spans := sel("shed_span_duration_seconds_count", shedSelf)
	b.add(12, 8, timeseries("Requests/s by route", "reqps",
		metric(fmt.Sprintf("topk(15, sum by (span_name) (%s))", rate(spans)), "{{span_name}}")))
	b.add(12, 8, timeseries("p95 latency by route", "s",
		metric(fmt.Sprintf("topk(15, %s)", quantile(0.95, "span_name, le", sel("shed_span_duration_seconds_bucket", shedSelf))), "{{span_name}}")))
	b.add(12, 8, timeseries("Errors/s by route", "reqps",
		metric(fmt.Sprintf("sum by (span_name) (%s)", rate(sel("shed_span_duration_seconds_count", shedSelf, `status_code="error"`))), "{{span_name}}")))
	b.add(12, 8, timeseries("MCP tool calls/s by agent", "ops",
		metric(fmt.Sprintf("sum by (client, tool) (%s)", rate(sel("shed_mcp_tool_calls_total", shedSelf))), "{{client}} {{tool}}")))
	b.add(24, 8, timeseries("Webhook deliveries/s", "ops",
		metric(fmt.Sprintf("sum by (event, outcome) (%s)", rate(sel("shed_webhook_deliveries_total", shedSelf))), "{{event}} {{outcome}}")))
}

func shedProxy(b *board, _ string) {
	b.add(12, 8, timeseries("Edge requests/s by status", "reqps", metric(statusClass("shed_http_requests_total"), "{{class}}")))
	b.add(12, 8, timeseries("Edge requests/s by host", "reqps",
		metric(fmt.Sprintf("topk(10, sum by (host) (%s))", rate("shed_http_requests_total")), "{{host}}")))
	b.add(8, 8, timeseries("Edge p95 latency", "s", metric(quantile(0.95, "le", "shed_http_request_duration_seconds_bucket"), "p95")))
	b.add(8, 8, timeseries("Edge bytes/s", "Bps", metric(fmt.Sprintf("sum(%s)", rate("shed_http_response_bytes_total")), "sent")))
	b.add(8, 8, timeseries("Route apply duration p95", "s",
		metric(quantile(0.95, "le", sel("shed_route_apply_duration_seconds_bucket", shedSelf)), "p95")))
}

func shedHost(b *board, _ string) {
	hostUsage(b)
	b.add(8, 8, timeseries("Host network", "Bps",
		metric("max(shed_host_network_rx_bytes_per_second)", "received"),
		metric("max(shed_host_network_tx_bytes_per_second)", "sent"),
	))
	b.add(8, 8, timeseries("Host disk I/O", "Bps",
		metric("max(shed_host_disk_read_bytes_per_second)", "read"),
		metric("max(shed_host_disk_write_bytes_per_second)", "written"),
	))
	b.add(8, 8, timeseries("Host disk used", "bytes",
		metric("max(shed_host_disk_used_bytes)", "used"),
		metric("max(shed_host_disk_total_bytes)", "total"),
	).limitLine("B"))
	b.add(12, 8, timeseries("Containers by project", "short",
		metric("count by (shed_project_name) (shed_container_memory_bytes)", "{{shed_project_name}}")))
	b.add(12, 8, timeseries("Docker API p95 by operation", "s",
		metric(quantile(0.95, "op, le", sel("shed_docker_request_duration_seconds_bucket", shedSelf)), "{{op}}")))
}

func shedBackups(b *board, _ string) {
	mean := func(name string) string {
		return fmt.Sprintf("sum(%s) / sum(%s)", rate(sel(name+"_sum", shedRecorded)), rate(sel(name+"_count", shedRecorded)))
	}
	b.add(12, 8, bars("Backups by outcome", "short",
		metric(fmt.Sprintf("sum by (kind, outcome) (%s)", increase(sel("shed_backups_total", shedRecorded), "$__rate_interval")), "{{kind}} {{outcome}}")))
	b.add(12, 8, stat("Failures", "short",
		metricNow(fmt.Sprintf("sum(%s)", increase(sel("shed_backups_total", shedRecorded, `outcome="failed"`), "$__range")), ""), "0"))
	b.add(12, 8, timeseries("Mean duration", "s", metric(mean("shed_backup_duration_seconds"), "duration")))
	b.add(12, 8, timeseries("Mean size", "bytes", metric(mean("shed_backup_bytes"), "size")))
	b.add(24, 10, logsPanel("Backup and upload log", logLines(shedLogs+" (backup or upload)")))
}

func shedRuntime(b *board, _ string) {
	b.add(12, 8, timeseries("Goroutines", "short", metric(sel("go_goroutine_count", shedSelf), "goroutines")))
	b.add(12, 8, timeseries("Heap", "bytes",
		metric(fmt.Sprintf("sum by (go_memory_type) (%s)", sel("go_memory_used_bytes", shedSelf)), "{{go_memory_type}}"),
		metric(sel("go_memory_gc_goal_bytes", shedSelf), "GC goal"),
	))
	b.add(8, 8, timeseries("Allocations", "Bps", metric(rate(sel("go_memory_allocated_bytes_total", shedSelf)), "allocated")))
	b.add(8, 8, timeseries("Scheduling latency p95", "s",
		metric(quantile(0.95, "le", sel("go_schedule_duration_seconds_bucket", shedSelf)), "p95")))
	b.add(8, 8, timeseries("Store query latency", "s",
		metric(quantile(0.5, "le", sel("shed_store_query_duration_seconds_bucket", shedSelf)), "p50"),
		metric(quantile(0.99, "le", sel("shed_store_query_duration_seconds_bucket", shedSelf)), "p99"),
	))
}

func shedTelemetry(b *board, _ string) {
	b.add(12, 8, timeseries("Ingested/s by signal", "ops",
		metric(fmt.Sprintf("sum by (signal) (%s)", rate(sel("shed_telemetry_ingested_total", shedSelf))), "{{signal}}")))
	b.add(12, 8, timeseries("Dropped/s by reason", "ops",
		metric(fmt.Sprintf("sum by (signal, reason) (%s)", rate("shed_telemetry_dropped_total")), "{{signal}} {{reason}}")))
	b.add(12, 8, timeseries("Dropped/s by service", "ops",
		metric(fmt.Sprintf("topk(10, sum by (shed_service_name) (%s))", rate("shed_telemetry_dropped_total")), "{{shed_service_name}}")))
	b.add(12, 8, timeseries("Store disk", "bytes",
		metric("sum(vm_data_size_bytes)", "metrics"),
		metric("sum(vl_data_size_bytes)", "logs"),
		metric("sum(vt_data_size_bytes)", "traces"),
	))
}

func shedSecurity(b *board, _ string) {
	audit := stream(eq("shed.source", "audit"))
	edge := stream(eq("shed.source", "edge"))
	b.add(24, 10, logsPanel("Audit timeline", logLines(audit)))
	b.add(12, 8, bars("Sign-ins by outcome", "short",
		metric(fmt.Sprintf("sum by (outcome) (%s)", increase(sel("shed_signins_total", shedSelf), "$__rate_interval")), "{{outcome}}")))
	b.add(12, 8, top("Sign-ins by IP", "short",
		logTotals(audit+` action:in("auth.signin", "auth.signin.denied") | stats by (actor_ip, action) count() signins | sort by (signins desc) | limit 10`, "{{actor_ip}} {{action}}")))
	b.add(12, 8, bars("Webhook signature failures", "short",
		logSeries(audit+` action:="webhook.signature.invalid" | stats count() failures`, "failures")))
	b.add(12, 8, top("MCP calls per agent", "short",
		metricNow(fmt.Sprintf("sum by (client) (%s)", increase(sel("shed_mcp_tool_calls_total", shedSelf), "$__range")), "{{client}}")))
	b.add(12, 10, logsPanel("Container exec and OOM", logLines(audit+` action:in("container.exec", "container.oom")`)))
	b.add(12, 10, logsPanel("Egress to new destinations", logLines(stream(eq("shed.source", "egress"))+` "egress.first_seen"`)))
	b.add(8, 10, top("Edge top client IPs", "short",
		logTotals(edge+" | stats by (client_ip) count() requests | sort by (requests desc) | limit 10", "{{client_ip}}")))
	b.add(8, 10, timeseries("4xx/s by host", "reqps",
		metric(fmt.Sprintf("topk(10, sum by (host) (%s))", rate(`shed_http_requests_total{code=~"4.."}`)), "{{host}}")))
	b.add(8, 10, top("404 paths (scanners)", "short",
		logTotals(edge+" status:=404 | stats by (path) count() hits | sort by (hits desc) | limit 15", "{{path}}")))
}
