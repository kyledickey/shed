package victoria

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Metrics is a VictoriaMetrics client.
type Metrics struct {
	b base
}

// NewMetrics returns a client for the VictoriaMetrics at baseURL, such as
// "http://127.0.0.1:8428". A nil hc gets a client with a 30 s timeout.
func NewMetrics(baseURL string, hc *http.Client, opts ...Option) *Metrics {
	return &Metrics{b: newBase(baseURL, hc, "vm_data_size_bytes", opts)}
}

// Sample is one point of a series. Value is NaN when the store reports "NaN".
type Sample struct {
	Time  time.Time
	Value float64
}

// Series is a labeled sequence of samples. An instant query returns one
// sample per series.
type Series struct {
	Labels map[string]string
	Values []Sample
}

// Result is the answer to a MetricsQL query.
type Result struct {
	// Type is "vector", "matrix", or "scalar".
	Type   string
	Series []Series
}

// ImportPrometheus writes samples given as Prometheus text exposition lines,
// such as `shed_container_cpu_percent{shed_service_id="s1"} 12.5 1700000000000`.
// A line without a timestamp is stamped by the store on arrival.
func (m *Metrics) ImportPrometheus(ctx context.Context, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	body := strings.Join(lines, "\n") + "\n"
	return m.b.send(ctx, request{
		op:          "metrics import",
		method:      http.MethodPost,
		path:        "/api/v1/import/prometheus",
		body:        []byte(body),
		contentType: "text/plain",
	})
}

// ForwardOTLP sends an OTLP/HTTP metrics request, protobuf-encoded and
// uncompressed, to the store unchanged.
func (m *Metrics) ForwardOTLP(ctx context.Context, body []byte) error {
	return m.b.send(ctx, request{
		op:          "metrics otlp",
		method:      http.MethodPost,
		path:        "/opentelemetry/v1/metrics",
		body:        body,
		contentType: "application/x-protobuf",
	})
}

// Query evaluates a MetricsQL expression at t. A zero t means now.
func (m *Metrics) Query(ctx context.Context, q string, t time.Time) (*Result, error) {
	form := url.Values{"query": {q}}
	if !t.IsZero() {
		form.Set("time", unixSeconds(t))
	}
	return m.query(ctx, "metrics query", "/api/v1/query", form)
}

// QueryRange evaluates a MetricsQL expression at every step from start to end.
func (m *Metrics) QueryRange(ctx context.Context, q string, start, end time.Time, step time.Duration) (*Result, error) {
	form := url.Values{
		"query": {q},
		"start": {unixSeconds(start)},
		"end":   {unixSeconds(end)},
		"step":  {strconv.FormatInt(step.Milliseconds(), 10) + "ms"},
	}
	return m.query(ctx, "metrics query_range", "/api/v1/query_range", form)
}

// Health reports whether the store answers /health.
func (m *Metrics) Health(ctx context.Context) error {
	return m.b.health(ctx, "metrics health")
}

// DataSize returns the bytes of data the store holds on disk, from its
// /metrics page.
func (m *Metrics) DataSize(ctx context.Context) (int64, error) {
	return m.b.dataSize(ctx, "metrics stats")
}

func (m *Metrics) query(ctx context.Context, op, path string, form url.Values) (*Result, error) {
	data, err := m.b.get(ctx, request{op: op, method: http.MethodPost, path: path, form: form})
	if err != nil {
		return nil, err
	}
	res, err := parseResult(data)
	if err != nil {
		return nil, fmt.Errorf("victoria: %s: %w", op, err)
	}
	return res, nil
}

// unixSeconds formats t as Unix seconds with millisecond precision.
func unixSeconds(t time.Time) string {
	ms := t.UnixMilli()
	return fmt.Sprintf("%d.%03d", ms/1000, ms%1000)
}

type promResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

type promSeries struct {
	Metric map[string]string `json:"metric"`
	Value  *promPoint        `json:"value"`
	Values []promPoint       `json:"values"`
}

// promPoint decodes `[<unix seconds>, "<value>"]`.
type promPoint Sample

func (p *promPoint) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("sample: %w", err)
	}
	var ts float64
	if err := json.Unmarshal(raw[0], &ts); err != nil {
		return fmt.Errorf("sample time: %w", err)
	}
	var s string
	if err := json.Unmarshal(raw[1], &s); err != nil {
		return fmt.Errorf("sample value: %w", err)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("sample value %q: %w", s, err)
	}
	p.Time = time.UnixMilli(int64(math.Round(ts * 1000))).UTC()
	p.Value = v
	return nil
}

func parseResult(data []byte) (*Result, error) {
	var r promResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if r.Status != "success" {
		return nil, fmt.Errorf("status %q: %s: %s", r.Status, r.ErrorType, r.Error)
	}
	res := &Result{Type: r.Data.ResultType, Series: []Series{}}
	switch r.Data.ResultType {
	case "scalar":
		var p promPoint
		if err := json.Unmarshal(r.Data.Result, &p); err != nil {
			return nil, fmt.Errorf("decode scalar: %w", err)
		}
		res.Series = append(res.Series, Series{Labels: map[string]string{}, Values: []Sample{Sample(p)}})
	case "vector", "matrix":
		var raw []promSeries
		if err := json.Unmarshal(r.Data.Result, &raw); err != nil {
			return nil, fmt.Errorf("decode %s: %w", r.Data.ResultType, err)
		}
		for _, s := range raw {
			out := Series{Labels: s.Metric, Values: make([]Sample, 0, len(s.Values)+1)}
			if out.Labels == nil {
				out.Labels = map[string]string{}
			}
			if s.Value != nil {
				out.Values = append(out.Values, Sample(*s.Value))
			}
			for _, p := range s.Values {
				out.Values = append(out.Values, Sample(p))
			}
			res.Series = append(res.Series, out)
		}
	default:
		return nil, fmt.Errorf("unsupported result type %q", r.Data.ResultType)
	}
	return res, nil
}
