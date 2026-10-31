package grafana

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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

// WriteAlertRules writes the alert rules file under dir, applying overrides,
// which are keyed by rule UID. Grafana puts the rules in the root folder titled
// "shed" and creates that folder, with a random UID, if it is missing; so write
// the file after creating folder [ShedFolder], then call
// [Client.ReloadAlertRules].
func WriteAlertRules(dir string, overrides map[string]AlertOverride) error {
	return writeYAML(filepath.Join(dir, "alerting", "shed.yaml"), alertRulesFile(overrides))
}

// WriteWebhook writes the file that sends every alert to url with token as its
// bearer token: one contact point and the root notification policy. A reload
// replaces the whole policy tree. Apply it with [Client.ReloadAlertRules].
func WriteWebhook(dir, url, token string) error {
	return writeYAML(filepath.Join(dir, "alerting", "notify.yaml"), webhookFile(url, token))
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

// AlertRule is one provisioned alert rule. It fires while its query is above
// the threshold for the duration.
type AlertRule struct {
	UID         string
	Title       string
	Description string
	// Threshold is the default threshold in Unit. Nil means it can't be
	// changed and the rule fires above 0.
	Threshold *float64
	Unit      string
	// Max is the largest allowed threshold; 0 means there is no limit.
	Max float64
	// ForSeconds is the default duration. Nil means it can't be changed and
	// the rule fires at once.
	ForSeconds *int

	expr string
	// scale divides the threshold to get the value of expr, as 100 does for a
	// percentage of a ratio. 0 means 1.
	scale float64
	// summary renders the alert's text. threshold includes its unit and during
	// is empty or " for 10 minutes".
	summary func(threshold, during string) string
}

// AlertOverride is a change to an [AlertRule]'s defaults. A nil Threshold or
// ForSeconds means the default.
type AlertOverride struct {
	Paused     bool
	Threshold  *float64
	ForSeconds *int
}

// byService groups an aggregation by the service labels the summaries use.
const byService = "by (shed_service_id, shed_service_name)"

// removedAlertRules are rules that older versions provisioned. Grafana keeps a
// rule removed from a file unless the file deletes it.
var removedAlertRules = []string{"shed-restarts", "shed-oom", "shed-deploy-failed", "shed-backup-failed"}

func ptr[T any](v T) *T { return &v }

// AlertRules returns the provisioned alert rules with their defaults.
func AlertRules() []AlertRule {
	const svc = `{{ $labels.shed_service_name }}`
	dbConnections := fmt.Sprintf(`(sum %[1]s (pg_stat_activity_count) / max %[1]s (pg_settings_max_connections))
or (max %[1]s (mysql_global_status_threads_connected) / max %[1]s (mysql_global_variables_max_connections))
or (max %[1]s (mongodb_ss_connections{conn_type="current"}) / (max %[1]s (mongodb_ss_connections{conn_type="current"}) + max %[1]s (mongodb_ss_connections{conn_type="available"})))
or (max %[1]s (redis_connected_clients) / max %[1]s (redis_max_clients))`, byService)
	return []AlertRule{
		{
			UID: "shed-host-disk", Title: "Host disk almost full",
			Description: "The host disk is fuller than the threshold.",
			Threshold:   ptr(85.0), Unit: "%", Max: 100, scale: 100,
			expr: `max(shed_host_disk_used_bytes) / max(shed_host_disk_total_bytes)`,
			summary: func(th, _ string) string {
				return "the host disk is more than " + th + " full"
			},
		},
		{
			UID: "shed-host-memory", Title: "Host memory high",
			Description: "Host memory use stays above the threshold.",
			Threshold:   ptr(90.0), Unit: "%", Max: 100, scale: 100, ForSeconds: ptr(600),
			expr: `max(shed_host_memory_bytes) / max(shed_host_memory_total_bytes)`,
			summary: func(th, during string) string {
				return "host memory has been above " + th + during
			},
		},
		{
			UID: "shed-http-errors", Title: "HTTP 5xx ratio high",
			Description: "The share of a service's requests that fail with 5xx stays above the threshold, at more than 1 request a second.",
			Threshold:   ptr(5.0), Unit: "%", Max: 100, scale: 100, ForSeconds: ptr(300),
			expr: fmt.Sprintf(`(sum %[1]s (rate(shed_http_requests_total{code=~"5.."}[5m])) / sum %[1]s (rate(shed_http_requests_total[5m])))
and (sum %[1]s (rate(shed_http_requests_total[5m])) > 1)`, byService),
			summary: func(th, during string) string {
				return "more than " + th + " of requests to " + svc + " have failed with 5xx" + during
			},
		},
		{
			UID: "shed-http-latency", Title: "HTTP p95 latency high",
			Description: "A service's p95 latency stays above the threshold.",
			Threshold:   ptr(2.0), Unit: "s", ForSeconds: ptr(600),
			expr: `histogram_quantile(0.95, sum by (shed_service_id, shed_service_name, le) (rate(shed_http_request_duration_seconds_bucket[5m])))`,
			summary: func(th, during string) string {
				return "p95 latency of " + svc + " has been above " + th + during
			},
		},
		{
			UID: "shed-db-connections", Title: "Database connections high",
			Description: "A database stays above the threshold share of its maximum connections.",
			Threshold:   ptr(80.0), Unit: "%", Max: 100, scale: 100, ForSeconds: ptr(300),
			expr: dbConnections,
			summary: func(th, during string) string {
				return svc + " has used more than " + th + " of its maximum connections" + during
			},
		},
		{
			UID: "shed-telemetry-drops", Title: "Telemetry dropping records",
			Description: "shed keeps dropping a service's telemetry, usually because it is over its caps.",
			ForSeconds:  ptr(300),
			expr:        fmt.Sprintf(`sum %s (rate(shed_telemetry_dropped_total[5m]))`, byService),
			summary: func(_, during string) string {
				return "shed has been dropping telemetry from " + svc + during
			},
		},
	}
}

// render applies o to r's defaults.
func (r AlertRule) render(o AlertOverride) alertRuleEntry {
	threshold, thresholdText := 0.0, ""
	if r.Threshold != nil {
		v := *r.Threshold
		if o.Threshold != nil {
			v = *o.Threshold
		}
		threshold = v
		if r.scale != 0 {
			threshold = v / r.scale
		}
		thresholdText = strconv.FormatFloat(v, 'f', -1, 64)
		if r.Unit != "%" {
			thresholdText += " "
		}
		thresholdText += r.Unit
	}
	seconds := 0
	if r.ForSeconds != nil {
		seconds = *r.ForSeconds
		if o.ForSeconds != nil {
			seconds = *o.ForSeconds
		}
	}
	during := ""
	if seconds > 0 {
		during = " for " + durationText(seconds)
	}
	return alertRuleEntry{
		UID:       r.UID,
		Title:     r.Title,
		Condition: "C",
		Data: []alertQuery{
			{
				RefID:             "A",
				RelativeTimeRange: relativeTimeRange{From: 600},
				DatasourceUID:     MetricsUID,
				Model:             map[string]any{"refId": "A", "expr": r.expr, "instant": true},
			},
			{
				RefID:         "C",
				DatasourceUID: "__expr__",
				Model: map[string]any{
					"refId":      "C",
					"type":       "threshold",
					"expression": "A",
					"conditions": []map[string]any{{
						"evaluator": map[string]any{"type": "gt", "params": []float64{threshold}},
					}},
				},
			},
		},
		NoDataState:  "OK",
		ExecErrState: "Error",
		For:          strconv.Itoa(seconds) + "s",
		IsPaused:     o.Paused,
		Labels:       map[string]string{"shed": "default"},
		Annotations:  map[string]string{"summary": r.summary(thresholdText, during)},
	}
}

// durationText spells out seconds in the largest unit that divides it.
func durationText(seconds int) string {
	n, unit := seconds, "second"
	switch {
	case seconds%3600 == 0:
		n, unit = seconds/3600, "hour"
	case seconds%60 == 0:
		n, unit = seconds/60, "minute"
	}
	if n != 1 {
		unit += "s"
	}
	return strconv.Itoa(n) + " " + unit
}

type alertFile struct {
	APIVersion  int               `yaml:"apiVersion"`
	Groups      []alertGroup      `yaml:"groups"`
	DeleteRules []alertRuleDelete `yaml:"deleteRules"`
}

type alertRuleDelete struct {
	OrgID int    `yaml:"orgId"`
	UID   string `yaml:"uid"`
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
	IsPaused     bool              `yaml:"isPaused"`
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

func alertRulesFile(overrides map[string]AlertOverride) alertFile {
	rules := AlertRules()
	entries := make([]alertRuleEntry, len(rules))
	for i, r := range rules {
		entries[i] = r.render(overrides[r.UID])
	}
	deletes := make([]alertRuleDelete, len(removedAlertRules))
	for i, uid := range removedAlertRules {
		deletes[i] = alertRuleDelete{OrgID: 1, UID: uid}
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
		DeleteRules: deletes,
	}
}

// contactPointName is the name of the webhook contact point.
const contactPointName = "shed"

type webhookFileYAML struct {
	APIVersion    int                `yaml:"apiVersion"`
	ContactPoints []contactPointYAML `yaml:"contactPoints"`
	Policies      []policyYAML       `yaml:"policies"`
}

type contactPointYAML struct {
	OrgID     int            `yaml:"orgId"`
	Name      string         `yaml:"name"`
	Receivers []receiverYAML `yaml:"receivers"`
}

type receiverYAML struct {
	UID      string            `yaml:"uid"`
	Type     string            `yaml:"type"`
	Settings map[string]string `yaml:"settings"`
}

type policyYAML struct {
	OrgID          int      `yaml:"orgId"`
	Receiver       string   `yaml:"receiver"`
	GroupBy        []string `yaml:"group_by"`
	GroupWait      string   `yaml:"group_wait"`
	GroupInterval  string   `yaml:"group_interval"`
	RepeatInterval string   `yaml:"repeat_interval"`
}

func webhookFile(url, token string) webhookFileYAML {
	return webhookFileYAML{
		APIVersion: 1,
		ContactPoints: []contactPointYAML{{
			OrgID: 1,
			Name:  contactPointName,
			Receivers: []receiverYAML{{
				UID:  "shed-webhook",
				Type: "webhook",
				Settings: map[string]string{
					"url":                       url,
					"authorization_scheme":      "Bearer",
					"authorization_credentials": token,
				},
			}},
		}},
		Policies: []policyYAML{{
			OrgID:          1,
			Receiver:       contactPointName,
			GroupBy:        []string{"alertname", "shed_service_id"},
			GroupWait:      "30s",
			GroupInterval:  "5m",
			RepeatInterval: "4h",
		}},
	}
}
