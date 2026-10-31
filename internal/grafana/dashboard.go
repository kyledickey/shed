package grafana

import "fmt"

// This file models the parts of Grafana's dashboard JSON that the
// templates use, with small constructors that keep templates readable.

type dashboard struct {
	UID           string      `json:"uid"`
	Title         string      `json:"title"`
	Tags          []string    `json:"tags"`
	Editable      bool        `json:"editable"`
	SchemaVersion int         `json:"schemaVersion"`
	Time          timeRange   `json:"time"`
	Refresh       string      `json:"refresh"`
	Annotations   annotations `json:"annotations"`
	Templating    templating  `json:"templating"`
	Links         []link      `json:"links"`
	Panels        []panel     `json:"panels"`
}

// link is a dashboard link, or a data link when in a field's links.
type link struct {
	Title       string `json:"title"`
	Type        string `json:"type,omitempty"`
	URL         string `json:"url"`
	KeepTime    bool   `json:"keepTime,omitempty"`
	TargetBlank bool   `json:"targetBlank"`
}

type timeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type annotations struct {
	List []annotationQuery `json:"list"`
}

type annotationQuery struct {
	Name       string           `json:"name"`
	Datasource datasourceRef    `json:"datasource"`
	Enable     bool             `json:"enable"`
	IconColor  string           `json:"iconColor"`
	Target     annotationTarget `json:"target"`
}

type annotationTarget struct {
	Type     string   `json:"type"`
	Tags     []string `json:"tags"`
	MatchAny bool     `json:"matchAny"`
	Limit    int      `json:"limit"`
}

type templating struct {
	List []variable `json:"list"`
}

type variable struct {
	Name       string         `json:"name"`
	Label      string         `json:"label,omitempty"`
	Type       string         `json:"type"`
	Datasource *datasourceRef `json:"datasource,omitempty"`
	Query      any            `json:"query"`
	Definition string         `json:"definition,omitempty"`
	Refresh    int            `json:"refresh,omitempty"`
	IncludeAll bool           `json:"includeAll"`
	AllValue   string         `json:"allValue,omitempty"`
	Current    *current       `json:"current,omitempty"`
}

type current struct {
	Text  string `json:"text"`
	Value string `json:"value"`
}

type datasourceRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

var (
	metricsDS = datasourceRef{Type: MetricsPlugin, UID: MetricsUID}
	logsDS    = datasourceRef{Type: LogsPlugin, UID: LogsUID}
	tracesDS  = datasourceRef{Type: "jaeger", UID: TracesUID}
	mixedDS   = datasourceRef{Type: "datasource", UID: "-- Mixed --"}
	grafanaDS = datasourceRef{Type: "grafana", UID: "-- Grafana --"}
)

type panel struct {
	ID              int              `json:"id"`
	Type            string           `json:"type"`
	Title           string           `json:"title"`
	GridPos         gridPos          `json:"gridPos"`
	Datasource      *datasourceRef   `json:"datasource,omitempty"`
	Targets         []target         `json:"targets,omitempty"`
	FieldConfig     *fieldConfig     `json:"fieldConfig,omitempty"`
	Options         map[string]any   `json:"options,omitempty"`
	Transformations []transformation `json:"transformations,omitempty"`
	Collapsed       *bool            `json:"collapsed,omitempty"`
	Panels          []panel          `json:"panels,omitempty"`
}

type gridPos struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type fieldConfig struct {
	Defaults  fieldDefaults `json:"defaults"`
	Overrides []override    `json:"overrides"`
}

type fieldDefaults struct {
	Unit     string           `json:"unit,omitempty"`
	Min      *float64         `json:"min,omitempty"`
	NoValue  string           `json:"noValue,omitempty"`
	Mappings []map[string]any `json:"mappings,omitempty"`
	Custom   map[string]any   `json:"custom,omitempty"`
}

type override struct {
	Matcher    matcher    `json:"matcher"`
	Properties []property `json:"properties"`
}

type matcher struct {
	ID      string `json:"id"`
	Options string `json:"options"`
}

type property struct {
	ID    string `json:"id"`
	Value any    `json:"value"`
}

type transformation struct {
	ID      string         `json:"id"`
	Options map[string]any `json:"options"`
}

// target is a query of any of the three data sources; each sets only its
// own fields.
type target struct {
	RefID        string        `json:"refId"`
	Datasource   datasourceRef `json:"datasource"`
	Expr         string        `json:"expr,omitempty"`
	LegendFormat string        `json:"legendFormat,omitempty"`
	Instant      bool          `json:"instant,omitempty"`
	Range        bool          `json:"range,omitempty"`
	Format       string        `json:"format,omitempty"`
	// QueryType is "instant", "stats", or "statsRange" for logs, and
	// "search" for traces.
	QueryType string `json:"queryType,omitempty"`
	// Service, Tags, and Limit are a trace search.
	Service string `json:"service,omitempty"`
	Tags    string `json:"tags,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// metric is a range query of the metrics store.
func metric(expr, legend string) target {
	return target{Datasource: metricsDS, Expr: expr, LegendFormat: legend, Range: true}
}

// metricNow is an instant query of the metrics store, as a table.
func metricNow(expr, legend string) target {
	return target{Datasource: metricsDS, Expr: expr, LegendFormat: legend, Instant: true, Format: "table"}
}

// logLines queries raw log records.
func logLines(expr string) target {
	return target{Datasource: logsDS, Expr: expr, QueryType: "instant"}
}

// logSeries is a LogsQL stats query over time; expr ends in a stats pipe.
func logSeries(expr, legend string) target {
	return target{Datasource: logsDS, Expr: expr, LegendFormat: legend, QueryType: "statsRange"}
}

// logTotals is a LogsQL stats query over the whole time range.
func logTotals(expr, legend string) target {
	return target{Datasource: logsDS, Expr: expr, LegendFormat: legend, QueryType: "stats"}
}

// traceSearch is a Jaeger trace search.
func traceSearch(service, tags string, limit int) target {
	return target{Datasource: tracesDS, QueryType: "search", Service: service, Tags: tags, Limit: limit}
}

func newPanel(typ, title, unit string, targets []target) panel {
	ds := targets[0].Datasource
	for i := range targets {
		targets[i].RefID = string(rune('A' + i))
		if targets[i].Datasource != ds {
			ds = mixedDS
		}
	}
	return panel{
		Type:        typ,
		Title:       title,
		Datasource:  &ds,
		Targets:     targets,
		FieldConfig: &fieldConfig{Defaults: fieldDefaults{Unit: unit}, Overrides: []override{}},
	}
}

func timeseries(title, unit string, targets ...target) panel {
	return newPanel("timeseries", title, unit, targets)
}

// bars is a time series drawn as stacked bars, for counts per interval.
func bars(title, unit string, targets ...target) panel {
	p := newPanel("timeseries", title, unit, targets)
	p.FieldConfig.Defaults.Custom = map[string]any{
		"drawStyle": "bars", "fillOpacity": 80, "stacking": map[string]any{"mode": "normal"},
	}
	return p
}

func stat(title, unit string, t target, noValue string) panel {
	t.Format = ""
	p := newPanel("stat", title, unit, []target{t})
	p.FieldConfig.Defaults.NoValue = noValue
	p.Options = map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}}, "graphMode": "none"}
	return p
}

// top is a horizontal bar gauge, for ranked totals such as top paths.
func top(title, unit string, t target) panel {
	t.Format = ""
	p := newPanel("bargauge", title, unit, []target{t})
	p.Options = map[string]any{
		"orientation":   "horizontal",
		"displayMode":   "basic",
		"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "values": false},
	}
	return p
}

func table(title string, targets ...target) panel {
	return newPanel("table", title, "", targets)
}

func logsPanel(title string, t target) panel {
	p := newPanel("logs", title, "", []target{t})
	p.FieldConfig = nil
	p.Options = map[string]any{"showTime": true, "wrapLogMessage": true, "sortOrder": "Descending", "enableLogDetails": true}
	return p
}

// limitLine draws the query with refID as a dashed red line, for a limit
// the other series are measured against.
func (p panel) limitLine(refID string) panel {
	p.FieldConfig.Overrides = append(p.FieldConfig.Overrides, override{
		Matcher: matcher{ID: "byFrameRefID", Options: refID},
		Properties: []property{
			{ID: "custom.lineStyle", Value: map[string]any{"fill": "dash", "dash": []int{10, 10}}},
			{ID: "color", Value: map[string]any{"mode": "fixed", "fixedColor": "red"}},
			{ID: "custom.fillOpacity", Value: 0},
		},
	})
	return p
}

// mapValue shows value as text in color.
func (p panel) mapValue(value, text, color string) panel {
	p.FieldConfig.Defaults.Mappings = append(p.FieldConfig.Defaults.Mappings, map[string]any{
		"type": "value", "options": map[string]any{value: map[string]any{"text": text, "color": color}},
	})
	return p
}

// hide hides the field named name, which data links can still read.
func (p panel) hide(name string) panel {
	p.FieldConfig.Overrides = append(p.FieldConfig.Overrides, override{
		Matcher:    matcher{ID: "byName", Options: name},
		Properties: []property{{ID: "custom.hidden", Value: true}},
	})
	return p
}

// linkOf links the values of the field named name to url, a path under
// Grafana's root that may use data link variables.
func (p panel) linkOf(name, title, url string) panel {
	p.FieldConfig.Overrides = append(p.FieldConfig.Overrides, override{
		Matcher:    matcher{ID: "byName", Options: name},
		Properties: []property{{ID: "links", Value: []link{{Title: title, URL: url}}}},
	})
	return p
}

// unitOf sets the unit of the field named name.
func (p panel) unitOf(name, unit string) panel {
	p.FieldConfig.Overrides = append(p.FieldConfig.Overrides, override{
		Matcher:    matcher{ID: "byName", Options: name},
		Properties: []property{{ID: "unit", Value: unit}},
	})
	return p
}

func (p panel) transform(id string, options map[string]any) panel {
	p.Transformations = append(p.Transformations, transformation{ID: id, Options: options})
	return p
}

// board lays panels out left to right in rows on Grafana's 24-column grid.
type board struct {
	d       dashboard
	x, y, h int
}

func newBoard(uid, title string, tags []string, annotationTags []string) *board {
	return &board{d: dashboard{
		UID:           uid,
		Title:         title,
		Tags:          tags,
		Editable:      true,
		SchemaVersion: 39,
		Time:          timeRange{From: "now-6h", To: "now"},
		Refresh:       "1m",
		Annotations: annotations{List: []annotationQuery{{
			Name:       "shed events",
			Datasource: grafanaDS,
			Enable:     true,
			IconColor:  "#3b9c5a",
			Target:     annotationTarget{Type: "tags", Tags: annotationTags, Limit: 100},
		}}},
		Templating: templating{List: []variable{}},
		Links:      []link{},
		Panels:     []panel{},
	}}
}

func (b *board) add(w, h int, p panel) {
	if b.x+w > 24 {
		b.newline()
	}
	p.ID = len(b.d.Panels) + 1
	p.GridPos = gridPos{X: b.x, Y: b.y, W: w, H: h}
	b.x += w
	b.h = max(b.h, h)
	b.d.Panels = append(b.d.Panels, p)
}

func (b *board) newline() {
	b.x, b.y, b.h = 0, b.y+b.h, 0
}

func (b *board) row(title string) {
	if b.x > 0 {
		b.newline()
	}
	collapsed := false
	b.add(24, 1, panel{Type: "row", Title: title, Collapsed: &collapsed, Panels: []panel{}})
	b.newline()
}

// link adds a link to the dashboard at url, a path under Grafana's root,
// that keeps the time range.
func (b *board) link(title, url string) {
	b.d.Links = append(b.d.Links, link{Title: title, Type: "link", URL: url, KeepTime: true})
}

// metricVar adds a variable whose values are those of label in the
// metrics matched by selector.
func (b *board) metricVar(name, label, selector string, includeAll bool) {
	q := fmt.Sprintf("label_values(%s, %s)", selector, label)
	v := variable{
		Name:       name,
		Type:       "query",
		Datasource: &datasourceRef{Type: MetricsPlugin, UID: MetricsUID},
		Query:      map[string]string{"query": q, "refId": "VariableQuery"},
		Definition: q,
		Refresh:    2,
		IncludeAll: includeAll,
	}
	if includeAll {
		v.AllValue = ".*"
	}
	b.d.Templating.List = append(b.d.Templating.List, v)
}

func (b *board) textVar(name, label, value string) {
	b.d.Templating.List = append(b.d.Templating.List, variable{
		Name: name, Label: label, Type: "textbox", Query: value, Current: &current{Text: value, Value: value},
	})
}
