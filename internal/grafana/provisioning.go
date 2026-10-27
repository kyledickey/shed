package grafana

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// The UIDs of the provisioned data sources.
const (
	MetricsUID = "shed-metrics"
	LogsUID    = "shed-logs"
	TracesUID  = "shed-traces"
)

// Stores holds the host:port addresses of the telemetry stores.
type Stores struct {
	Metrics string // VictoriaMetrics
	Logs    string // VictoriaLogs
	Traces  string // VictoriaTraces
}

// WriteDatasources writes the data sources file under dir, the directory
// mounted at [ProvisioningPath]. Grafana applies it on every start.
func WriteDatasources(dir string, s Stores) error {
	return writeYAML(filepath.Join(dir, "datasources", "shed.yaml"), datasourcesFile(s))
}

// WriteAlertRules writes the default alert rules file under dir. Grafana puts
// the rules in the root folder titled "shed" and creates that folder, with a
// random UID, if it is missing; so write the file after creating folder
// [ShedFolder], then call [Client.ReloadAlertRules].
func WriteAlertRules(dir string) error {
	return writeYAML(filepath.Join(dir, "alerting", "shed.yaml"), alertRulesFile())
}

func writeYAML(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("grafana: encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("grafana: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("grafana: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("grafana: %w", err)
	}
	return nil
}

type datasourceFile struct {
	APIVersion  int                `yaml:"apiVersion"`
	Prune       bool               `yaml:"prune"`
	Datasources []datasourceConfig `yaml:"datasources"`
}

type datasourceConfig struct {
	Name      string         `yaml:"name"`
	Type      string         `yaml:"type"`
	UID       string         `yaml:"uid"`
	URL       string         `yaml:"url"`
	Access    string         `yaml:"access"`
	IsDefault bool           `yaml:"isDefault"`
	JSONData  map[string]any `yaml:"jsonData,omitempty"`
}

func datasourcesFile(s Stores) datasourceFile {
	return datasourceFile{
		APIVersion: 1,
		Prune:      true,
		Datasources: []datasourceConfig{
			{
				Name: "VictoriaMetrics", Type: MetricsPlugin, UID: MetricsUID,
				URL: "http://" + s.Metrics, Access: "proxy", IsDefault: true,
			},
			{
				Name: "VictoriaLogs", Type: LogsPlugin, UID: LogsUID,
				URL: "http://" + s.Logs, Access: "proxy",
				JSONData: map[string]any{
					"derivedFields": []map[string]any{{
						"name":          "trace_id",
						"matcherType":   "label",
						"matcherRegex":  "trace_id",
						"datasourceUid": TracesUID,
						"url":           "${__value.raw}",
					}},
				},
			},
			{
				Name: "VictoriaTraces", Type: "jaeger", UID: TracesUID,
				URL: "http://" + s.Traces + "/select/jaeger", Access: "proxy",
				JSONData: map[string]any{
					"tracesToLogsV2": map[string]any{
						"datasourceUid":      LogsUID,
						"filterByTraceID":    false,
						"customQuery":        true,
						"query":              `trace_id:="${__trace.traceId}"`,
						"spanStartTimeShift": "-5m",
						"spanEndTimeShift":   "5m",
					},
				},
			},
		},
	}
}

// alertRule is one default rule: it fires while Expr, evaluated as an
// instant query, is above Threshold for For.
type alertRule struct {
	UID       string
	Title     string
	Expr      string
	Threshold float64
	For       string
	Summary   string
}

// byService groups an aggregation by the service labels the summaries use.
const byService = "by (shed_service_id, shed_service_name)"

func defaultAlertRules() []alertRule {
	const svc = `{{ $labels.shed_service_name }}`
	dbConnections := fmt.Sprintf(`(sum %[1]s (pg_stat_activity_count) / max %[1]s (pg_settings_max_connections))
or (max %[1]s (mysql_global_status_threads_connected) / max %[1]s (mysql_global_variables_max_connections))
or (max %[1]s (mongodb_ss_connections{conn_type="current"}) / (max %[1]s (mongodb_ss_connections{conn_type="current"}) + max %[1]s (mongodb_ss_connections{conn_type="available"})))
or (max %[1]s (redis_connected_clients) / max %[1]s (redis_max_clients))`, byService)
	return []alertRule{
		{
			UID: "shed-restarts", Title: "Service restarting",
			Expr:      fmt.Sprintf(`sum %s (increase(shed_container_restarts_total[10m]))`, byService),
			Threshold: 3, For: "0s",
			Summary: svc + " restarted more than 3 times in 10 minutes",
		},
		{
			UID: "shed-oom", Title: "Container OOM killed",
			Expr:      fmt.Sprintf(`sum %s (increase(shed_container_oom_kills_total[10m]))`, byService),
			Threshold: 0, For: "0s",
			Summary: "a container of " + svc + " ran out of memory",
		},
		{
			UID: "shed-host-disk", Title: "Host disk almost full",
			Expr:      `max(shed_host_disk_used_bytes) / max(shed_host_disk_total_bytes)`,
			Threshold: 0.85, For: "0s",
			Summary: "the host disk is more than 85% full",
		},
		{
			UID: "shed-host-memory", Title: "Host memory high",
			Expr:      `max(shed_host_memory_bytes) / max(shed_host_memory_total_bytes)`,
			Threshold: 0.9, For: "10m",
			Summary: "host memory has been above 90% for 10 minutes",
		},
		{
			UID: "shed-http-errors", Title: "HTTP 5xx ratio high",
			Expr: fmt.Sprintf(`(sum %[1]s (rate(shed_http_requests_total{code=~"5.."}[5m])) / sum %[1]s (rate(shed_http_requests_total[5m])))
and (sum %[1]s (rate(shed_http_requests_total[5m])) > 1)`, byService),
			Threshold: 0.05, For: "5m",
			Summary: "more than 5% of requests to " + svc + " fail with 5xx",
		},
		{
			UID: "shed-http-latency", Title: "HTTP p95 latency high",
			Expr:      `histogram_quantile(0.95, sum by (shed_service_id, shed_service_name, le) (rate(shed_http_request_duration_seconds_bucket[5m])))`,
			Threshold: 2, For: "10m",
			Summary: "p95 latency of " + svc + " has been above 2 s for 10 minutes",
		},
		{
			UID: "shed-db-connections", Title: "Database connections high",
			Expr:      dbConnections,
			Threshold: 0.8, For: "5m",
			Summary: svc + " uses more than 80% of its maximum connections",
		},
		{
			UID: "shed-deploy-failed", Title: "Deploy failed",
			Expr:      `sum(increase(shed_deployments_total{outcome="failed"}[10m]))`,
			Threshold: 0, For: "0s",
			Summary: "a deployment failed",
		},
		{
			UID: "shed-backup-failed", Title: "Backup failed",
			Expr:      `sum by (kind) (increase(shed_backups_total{outcome="failed"}[10m]))`,
			Threshold: 0, For: "0s",
			Summary: "a backup failed",
		},
		{
			UID: "shed-telemetry-drops", Title: "Telemetry dropping records",
			Expr:      fmt.Sprintf(`sum %s (rate(shed_telemetry_dropped_total[5m]))`, byService),
			Threshold: 0, For: "5m",
			Summary: "shed is dropping telemetry from " + svc,
		},
	}
}

type alertFile struct {
	APIVersion int          `yaml:"apiVersion"`
	Groups     []alertGroup `yaml:"groups"`
}

type alertGroup struct {
	OrgID    int              `yaml:"orgId"`
	Name     string           `yaml:"name"`
	Folder   string           `yaml:"folder"`
	Interval string           `yaml:"interval"`
	Rules    []alertRuleEntry `yaml:"rules"`
}

type alertRuleEntry struct {
	UID          string            `yaml:"uid"`
	Title        string            `yaml:"title"`
	Condition    string            `yaml:"condition"`
	Data         []alertQuery      `yaml:"data"`
	NoDataState  string            `yaml:"noDataState"`
	ExecErrState string            `yaml:"execErrState"`
	For          string            `yaml:"for"`
	Labels       map[string]string `yaml:"labels"`
	Annotations  map[string]string `yaml:"annotations"`
}

type alertQuery struct {
	RefID             string            `yaml:"refId"`
	RelativeTimeRange relativeTimeRange `yaml:"relativeTimeRange"`
	DatasourceUID     string            `yaml:"datasourceUid"`
	Model             map[string]any    `yaml:"model"`
}

type relativeTimeRange struct {
	From int `yaml:"from"`
	To   int `yaml:"to"`
}

func alertRulesFile() alertFile {
	rules := defaultAlertRules()
	entries := make([]alertRuleEntry, len(rules))
	for i, r := range rules {
		entries[i] = alertRuleEntry{
			UID:       r.UID,
			Title:     r.Title,
			Condition: "C",
			Data: []alertQuery{
				{
					RefID:             "A",
					RelativeTimeRange: relativeTimeRange{From: 600},
					DatasourceUID:     MetricsUID,
					Model:             map[string]any{"refId": "A", "expr": r.Expr, "instant": true},
				},
				{
					RefID:         "C",
					DatasourceUID: "__expr__",
					Model: map[string]any{
						"refId":      "C",
						"type":       "threshold",
						"expression": "A",
						"conditions": []map[string]any{{
							"evaluator": map[string]any{"type": "gt", "params": []float64{r.Threshold}},
						}},
					},
				},
			},
			NoDataState:  "OK",
			ExecErrState: "Error",
			For:          r.For,
			Labels:       map[string]string{"shed": "default"},
			Annotations:  map[string]string{"summary": r.Summary},
		}
	}
	return alertFile{
		APIVersion: 1,
		Groups: []alertGroup{{
			OrgID:    1,
			Name:     "shed-defaults",
			Folder:   "shed",
			Interval: "1m",
			Rules:    entries,
		}},
	}
}
