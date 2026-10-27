package victoria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrTraceNotFound is returned by [Traces.Trace] when the store has no spans
// for the trace ID.
var ErrTraceNotFound = errors.New("victoria: trace not found")

// Traces is a VictoriaTraces client.
type Traces struct {
	b base
}

// NewTraces returns a client for the VictoriaTraces at baseURL, such as
// "http://127.0.0.1:10428". A nil hc gets a client with a 30 s timeout.
func NewTraces(baseURL string, hc *http.Client, opts ...Option) *Traces {
	return &Traces{b: newBase(baseURL, hc, "vt_data_size_bytes", opts)}
}

// SearchParams filter a trace search. Zero values leave a filter out.
type SearchParams struct {
	Service     string
	Operation   string
	Tags        map[string]string
	MinDuration time.Duration
	MaxDuration time.Duration
	Start       time.Time
	End         time.Time
	// Limit is the number of traces to return. The store defaults to 20.
	Limit int
}

// TraceSummary is one search hit.
type TraceSummary struct {
	TraceID string
	// Service and Name are those of the root span, or of the earliest span
	// when the root is not stored.
	Service  string
	Name     string
	Start    time.Time
	Duration time.Duration
	Spans    int
	// Errors counts spans with an error status.
	Errors int
}

// Span is one span of a trace.
type Span struct {
	SpanID string
	// ParentID is empty for a root span.
	ParentID string
	Service  string
	Name     string
	// Kind is "internal", "server", "client", "producer", or "consumer".
	Kind     string
	Start    time.Time
	Duration time.Duration
	// Status is "error", "ok", or "unset".
	Status string
	// Attributes holds the span's tags as strings, without the kind and
	// status tags.
	Attributes map[string]string
}

// Trace is all the stored spans of one trace, ordered by start time.
type Trace struct {
	TraceID string
	Spans   []Span
}

// ForwardOTLP sends an OTLP/HTTP traces request, protobuf-encoded and
// uncompressed, to the store unchanged.
func (t *Traces) ForwardOTLP(ctx context.Context, body []byte) error {
	return t.b.send(ctx, request{
		op:          "traces otlp",
		method:      http.MethodPost,
		path:        "/insert/opentelemetry/v1/traces",
		body:        body,
		contentType: "application/x-protobuf",
	})
}

// SearchTraces finds traces through the Jaeger query API, newest first.
func (t *Traces) SearchTraces(ctx context.Context, p SearchParams) ([]TraceSummary, error) {
	q := url.Values{}
	if p.Service != "" {
		q.Set("service", p.Service)
	}
	if p.Operation != "" {
		q.Set("operation", p.Operation)
	}
	if len(p.Tags) > 0 {
		b, err := json.Marshal(p.Tags)
		if err != nil {
			return nil, fmt.Errorf("victoria: traces search: encode tags: %w", err)
		}
		q.Set("tags", string(b))
	}
	if p.MinDuration > 0 {
		q.Set("minDuration", strconv.FormatInt(p.MinDuration.Nanoseconds(), 10)+"ns")
	}
	if p.MaxDuration > 0 {
		q.Set("maxDuration", strconv.FormatInt(p.MaxDuration.Nanoseconds(), 10)+"ns")
	}
	if !p.Start.IsZero() {
		q.Set("start", strconv.FormatInt(p.Start.UnixMicro(), 10))
	}
	if !p.End.IsZero() {
		q.Set("end", strconv.FormatInt(p.End.UnixMicro(), 10))
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	traces, err := t.jaeger(ctx, "traces search", "/select/jaeger/api/traces", q)
	if err != nil {
		return nil, err
	}
	out := make([]TraceSummary, 0, len(traces))
	for _, jt := range traces {
		tr := jt.trace()
		out = append(out, summarize(tr))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out, nil
}

// Trace returns the spans of one trace through the Jaeger query API.
func (t *Traces) Trace(ctx context.Context, id string) (*Trace, error) {
	traces, err := t.jaeger(ctx, "traces get", "/select/jaeger/api/traces/"+url.PathEscape(id), nil)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		return nil, ErrTraceNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(traces) == 0 || len(traces[0].Spans) == 0 {
		return nil, ErrTraceNotFound
	}
	tr := traces[0].trace()
	return &tr, nil
}

// Health reports whether the store answers /health.
func (t *Traces) Health(ctx context.Context) error {
	return t.b.health(ctx, "traces health")
}

// DataSize returns the bytes of data the store holds on disk, from its
// /metrics page.
func (t *Traces) DataSize(ctx context.Context) (int64, error) {
	return t.b.dataSize(ctx, "traces stats")
}

func (t *Traces) jaeger(ctx context.Context, op, path string, q url.Values) ([]jaegerTrace, error) {
	data, err := t.b.get(ctx, request{op: op, method: http.MethodGet, path: path, query: q})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data   []jaegerTrace `json:"data"`
		Errors []struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("victoria: %s: decode: %w", op, err)
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("victoria: %s: %d: %s", op, resp.Errors[0].Code, resp.Errors[0].Msg)
	}
	return resp.Data, nil
}

type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Spans     []jaegerSpan             `json:"spans"`
	Processes map[string]jaegerProcess `json:"processes"`
}

type jaegerProcess struct {
	ServiceName string `json:"serviceName"`
}

type jaegerSpan struct {
	TraceID       string `json:"traceID"`
	SpanID        string `json:"spanID"`
	OperationName string `json:"operationName"`
	References    []struct {
		RefType string `json:"refType"`
		SpanID  string `json:"spanID"`
	} `json:"references"`
	StartTime int64       `json:"startTime"`
	Duration  int64       `json:"duration"`
	Tags      []jaegerTag `json:"tags"`
	ProcessID string      `json:"processID"`
}

type jaegerTag struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

// text renders a tag value: strings as they are, other JSON values as written.
func (g jaegerTag) text() string {
	var s string
	if json.Unmarshal(g.Value, &s) == nil {
		return s
	}
	return string(g.Value)
}

// trace converts Jaeger's model to a Trace, spans ordered by start time.
func (jt jaegerTrace) trace() Trace {
	tr := Trace{TraceID: jt.TraceID, Spans: make([]Span, 0, len(jt.Spans))}
	for _, js := range jt.Spans {
		if tr.TraceID == "" {
			tr.TraceID = js.TraceID
		}
		sp := Span{
			SpanID:     js.SpanID,
			Service:    jt.Processes[js.ProcessID].ServiceName,
			Name:       js.OperationName,
			Start:      time.UnixMicro(js.StartTime).UTC(),
			Duration:   time.Duration(js.Duration) * time.Microsecond,
			Status:     "unset",
			Attributes: map[string]string{},
		}
		for _, ref := range js.References {
			if ref.RefType == "CHILD_OF" || sp.ParentID == "" {
				sp.ParentID = ref.SpanID
			}
		}
		for _, tag := range js.Tags {
			switch tag.Key {
			case "span.kind":
				sp.Kind = tag.text()
			case "error":
				switch strings.ToLower(tag.text()) {
				case "true":
					sp.Status = "error"
				case "false":
					sp.Status = "ok"
				}
			default:
				sp.Attributes[tag.Key] = tag.text()
			}
		}
		tr.Spans = append(tr.Spans, sp)
	}
	sort.SliceStable(tr.Spans, func(i, j int) bool { return tr.Spans[i].Start.Before(tr.Spans[j].Start) })
	return tr
}

func summarize(tr Trace) TraceSummary {
	s := TraceSummary{TraceID: tr.TraceID, Spans: len(tr.Spans)}
	if len(tr.Spans) == 0 {
		return s
	}
	ids := make(map[string]bool, len(tr.Spans))
	for _, sp := range tr.Spans {
		ids[sp.SpanID] = true
		if sp.Status == "error" {
			s.Errors++
		}
	}
	// The root has no parent in the trace; the earliest span stands in when
	// the root was never stored.
	root := tr.Spans[0]
	for _, sp := range tr.Spans {
		if sp.ParentID == "" || !ids[sp.ParentID] {
			root = sp
			break
		}
	}
	s.Service, s.Name, s.Start, s.Duration = root.Service, root.Name, root.Start, root.Duration
	return s
}
