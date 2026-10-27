package otlp_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/kyledickey/shed/internal/otlp"
)

const (
	protobufType = "application/x-protobuf"
	jsonType     = "application/json"
)

var app = otlp.Source{
	ProjectID:    "p1",
	ProjectName:  "shop",
	ServiceID:    "s1",
	ServiceName:  "api",
	ServiceKind:  "app",
	DeploymentID: "d1",
}

type forwarder struct {
	mu                    sync.Mutex
	err                   error
	traces, metrics, logs [][]byte
}

func (f *forwarder) add(dst *[][]byte, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	*dst = append(*dst, b)
	return nil
}

func (f *forwarder) Traces(_ context.Context, b []byte) error  { return f.add(&f.traces, b) }
func (f *forwarder) Metrics(_ context.Context, b []byte) error { return f.add(&f.metrics, b) }
func (f *forwarder) Logs(_ context.Context, b []byte) error    { return f.add(&f.logs, b) }

// clock is a settable time source for caps.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

type fixture struct {
	h      *otlp.Handler
	fwd    *forwarder
	reader *sdkmetric.ManualReader
	clock  *clock
}

func newFixture(t *testing.T, configure func(*otlp.Config)) *fixture {
	t.Helper()
	f := &fixture{
		fwd:    &forwarder{},
		reader: sdkmetric.NewManualReader(),
		clock:  &clock{t: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)},
	}
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(f.reader)))
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	cfg := otlp.Config{
		Attribute: func(*http.Request) (otlp.Attribution, error) { return otlp.Attribution{Source: app}, nil },
		Forwarder: f.fwd,
		Now:       f.clock.now,
	}
	if configure != nil {
		configure(&cfg)
	}
	h, err := otlp.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.h = h
	return f
}

type request struct {
	method, path, contentType, encoding string
	body                                []byte
}

func (f *fixture) do(r request) *httptest.ResponseRecorder {
	req := httptest.NewRequest(cmpOr(r.method, http.MethodPost), r.path, bytes.NewReader(r.body))
	req.Header.Set("Content-Type", cmpOr(r.contentType, protobufType))
	if r.encoding != "" {
		req.Header.Set("Content-Encoding", r.encoding)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	return w
}

func (f *fixture) post(path string, m proto.Message) *httptest.ResponseRecorder {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return f.do(request{path: path, body: b})
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// counter returns the value of an int64 counter with exactly attrs.
func (f *fixture) counter(t *testing.T, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	want := attribute.NewSet(attrs...)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if dp.Attributes.Equals(&want) {
					return dp.Value
				}
			}
		}
	}
	return 0
}

func (f *fixture) dropped(t *testing.T, serviceID, signal, reason string) int64 {
	t.Helper()
	return f.counter(t, "shed_telemetry_dropped_total",
		attribute.String("shed_service_id", serviceID),
		attribute.String("signal", signal),
		attribute.String("reason", reason))
}

func str(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: strValue(value)}
}

func strValue(s string) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: s}}
}

func kvlist(kvs ...*commonpb.KeyValue) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_KvlistValue{KvlistValue: &commonpb.KeyValueList{Values: kvs}}}
}

func array(vs ...*commonpb.AnyValue) *commonpb.AnyValue {
	return &commonpb.AnyValue{Value: &commonpb.AnyValue_ArrayValue{ArrayValue: &commonpb.ArrayValue{Values: vs}}}
}

func spans(names ...string) *coltracepb.ExportTraceServiceRequest {
	ss := &tracepb.ScopeSpans{Scope: &commonpb.InstrumentationScope{Name: "test"}}
	for _, n := range names {
		ss.Spans = append(ss.Spans, &tracepb.Span{Name: n, Kind: tracepb.Span_SPAN_KIND_SERVER})
	}
	return &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource:   &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", "checkout")}},
		ScopeSpans: []*tracepb.ScopeSpans{ss},
	}}}
}

func nSpans(n int) *coltracepb.ExportTraceServiceRequest {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("span %d", i)
	}
	return spans(names...)
}

func nLogs(n int) *collogspb.ExportLogsServiceRequest {
	sl := &logspb.ScopeLogs{}
	for i := range n {
		sl.LogRecords = append(sl.LogRecords, &logspb.LogRecord{Body: strValue(fmt.Sprintf("line %d", i))})
	}
	return &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{sl}}}}
}

// gauge returns a request with one gauge data point per attribute set.
func gauge(name string, sets ...[]*commonpb.KeyValue) *colmetricspb.ExportMetricsServiceRequest {
	g := &metricspb.Gauge{}
	for _, attrs := range sets {
		g.DataPoints = append(g.DataPoints, &metricspb.NumberDataPoint{
			Attributes: attrs,
			Value:      &metricspb.NumberDataPoint_AsInt{AsInt: 1},
		})
	}
	return &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		ScopeMetrics: []*metricspb.ScopeMetrics{{Metrics: []*metricspb.Metric{{
			Name: name,
			Data: &metricspb.Metric_Gauge{Gauge: g},
		}}}},
	}}}
}

func unmarshal[M proto.Message](t *testing.T, b []byte, m M) M {
	t.Helper()
	if err := proto.Unmarshal(b, m); err != nil {
		t.Fatal(err)
	}
	return m
}

func lastTraces(t *testing.T, f *fixture) *coltracepb.ExportTraceServiceRequest {
	t.Helper()
	if len(f.fwd.traces) == 0 {
		t.Fatal("nothing forwarded")
	}
	return unmarshal(t, f.fwd.traces[len(f.fwd.traces)-1], &coltracepb.ExportTraceServiceRequest{})
}

func spanCount(req *coltracepb.ExportTraceServiceRequest) int {
	n := 0
	for _, rs := range req.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			n += len(ss.Spans)
		}
	}
	return n
}

func gzipped(b []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

const minimalTracesJSON = `{"resourceSpans":[{"scopeSpans":[{"spans":[
	{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b174","name":"GET /","kind":2},
	{"traceId":"5b8efff798038103d269b633813fc60c","spanId":"eee19b7ec3c1b175","name":"GET /a","kind":"SPAN_KIND_CLIENT"}
]}]}]}`

func TestContentTypes(t *testing.T) {
	pb := mustMarshal(nSpans(2))
	tests := []struct {
		name       string
		req        request
		wantStatus int
		wantType   string
	}{
		{"protobuf", request{path: "/v1/traces", body: pb}, 200, protobufType},
		{"protobuf gzip", request{path: "/v1/traces", body: gzipped(pb), encoding: "gzip"}, 200, protobufType},
		{"json", request{path: "/v1/traces", contentType: jsonType, body: []byte(minimalTracesJSON)}, 200, jsonType},
		{"json with charset", request{path: "/v1/traces", contentType: "application/json; charset=utf-8", body: []byte(minimalTracesJSON)}, 200, jsonType},
		{"json gzip", request{path: "/v1/traces", contentType: jsonType, body: gzipped([]byte(minimalTracesJSON)), encoding: "gzip"}, 200, jsonType},
		{"unsupported type", request{path: "/v1/traces", contentType: "text/plain", body: pb}, 415, ""},
		{"unsupported encoding", request{path: "/v1/traces", body: pb, encoding: "br"}, 415, protobufType},
		{"bad gzip", request{path: "/v1/traces", body: pb, encoding: "gzip"}, 400, protobufType},
		{"bad protobuf", request{path: "/v1/traces", body: []byte{0xff, 0xff, 0xff}}, 400, protobufType},
		{"bad json", request{path: "/v1/traces", contentType: jsonType, body: []byte(`{"resourceSpans":`)}, 400, jsonType},
		{"get", request{method: http.MethodGet, path: "/v1/traces"}, 405, ""},
		{"unknown path", request{path: "/v1/profiles", body: pb}, 404, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			w := f.do(tt.req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.wantStatus, w.Body)
			}
			if tt.wantType == "" {
				return
			}
			if got := w.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			var resp proto.Message = &coltracepb.ExportTraceServiceResponse{}
			if tt.wantStatus != 200 {
				resp = &statuspb.Status{}
			}
			var err error
			if tt.wantType == jsonType {
				err = protojson.Unmarshal(w.Body.Bytes(), resp)
			} else {
				err = proto.Unmarshal(w.Body.Bytes(), resp)
			}
			if err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if tt.wantStatus != 200 {
				if len(f.fwd.traces) != 0 {
					t.Fatal("forwarded a rejected request")
				}
				return
			}
			if got := spanCount(lastTraces(t, f)); got != 2 {
				t.Fatalf("forwarded %d spans, want 2", got)
			}
		})
	}
}

// sizedRequest returns a valid traces request whose protobuf encoding is
// exactly size bytes.
func sizedRequest(t *testing.T, size int) []byte {
	t.Helper()
	req := nSpans(1)
	span := req.ResourceSpans[0].ScopeSpans[0].Spans[0]
	pad := 0
	for range 10 {
		span.Attributes = []*commonpb.KeyValue{str("pad", strings.Repeat("x", pad))}
		b := mustMarshal(req)
		if len(b) == size {
			return b
		}
		pad += size - len(b)
	}
	t.Fatalf("could not size a request to %d bytes", size)
	return nil
}

func TestSizeLimit(t *testing.T) {
	atLimit := sizedRequest(t, otlp.MaxBodySize)
	overLimit := make([]byte, otlp.MaxBodySize+1)
	tests := []struct {
		name       string
		req        request
		wantStatus int
	}{
		{"at limit", request{path: "/v1/traces", body: atLimit}, 200},
		{"at limit decompressed", request{path: "/v1/traces", body: gzipped(atLimit), encoding: "gzip"}, 200},
		{"over limit", request{path: "/v1/traces", body: overLimit}, 413},
		{"over limit decompressed", request{path: "/v1/traces", body: gzipped(overLimit), encoding: "gzip"}, 413},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, nil)
			if w := f.do(tt.req); w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.wantStatus, w.Body)
			}
		})
	}
}

func TestAttributionErrors(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantStatus     int
		wantRetryAfter string
		wantDropped    int64
	}{
		{"unknown source", fmt.Errorf("lookup 172.18.0.9: %w", otlp.ErrUnknownSource), 403, "", 3},
		{"index unavailable", errors.New("index not ready"), 503, "5", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, func(c *otlp.Config) {
				c.Attribute = func(*http.Request) (otlp.Attribution, error) { return otlp.Attribution{}, tt.err }
			})
			w := f.post("/v1/traces", nSpans(3))
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if got := w.Header().Get("Retry-After"); got != tt.wantRetryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tt.wantRetryAfter)
			}
			if len(f.fwd.traces) != 0 {
				t.Error("forwarded a request from an unattributed source")
			}
			if got := f.dropped(t, "", "traces", "unknown_source"); got != tt.wantDropped {
				t.Errorf("dropped = %d, want %d", got, tt.wantDropped)
			}
		})
	}
}

// signalCase adapts a test to each of the three signals.
type signalCase struct {
	path string
	// build returns a request with one record under res.
	build func(res *resourcepb.Resource) proto.Message
	// resources decodes the forwarded body's resources.
	resources func(t *testing.T, f *fixture) []*resourcepb.Resource
}

var signalCases = map[string]signalCase{
	"traces": {
		path: "/v1/traces",
		build: func(res *resourcepb.Resource) proto.Message {
			req := nSpans(1)
			req.ResourceSpans[0].Resource = res
			return req
		},
		resources: func(t *testing.T, f *fixture) []*resourcepb.Resource {
			var out []*resourcepb.Resource
			for _, rs := range lastTraces(t, f).ResourceSpans {
				out = append(out, rs.Resource)
			}
			return out
		},
	},
	"metrics": {
		path: "/v1/metrics",
		build: func(res *resourcepb.Resource) proto.Message {
			req := gauge("queue_depth", nil)
			req.ResourceMetrics[0].Resource = res
			return req
		},
		resources: func(t *testing.T, f *fixture) []*resourcepb.Resource {
			req := unmarshal(t, f.fwd.metrics[len(f.fwd.metrics)-1], &colmetricspb.ExportMetricsServiceRequest{})
			var out []*resourcepb.Resource
			for _, rm := range req.ResourceMetrics {
				out = append(out, rm.Resource)
			}
			return out
		},
	},
	"logs": {
		path: "/v1/logs",
		build: func(res *resourcepb.Resource) proto.Message {
			req := nLogs(1)
			req.ResourceLogs[0].Resource = res
			return req
		},
		resources: func(t *testing.T, f *fixture) []*resourcepb.Resource {
			req := unmarshal(t, f.fwd.logs[len(f.fwd.logs)-1], &collogspb.ExportLogsServiceRequest{})
			var out []*resourcepb.Resource
			for _, rl := range req.ResourceLogs {
				out = append(out, rl.Resource)
			}
			return out
		},
	},
}

// attrMap flattens string attributes, failing on duplicate keys.
func attrMap(t *testing.T, kvs []*commonpb.KeyValue) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, kv := range kvs {
		if _, dup := m[kv.Key]; dup {
			t.Errorf("attribute %q appears twice", kv.Key)
		}
		m[kv.Key] = kv.GetValue().GetStringValue()
	}
	return m
}

func TestResourceStamping(t *testing.T) {
	stamped := func(src otlp.Source, serviceName string, extra map[string]string) map[string]string {
		m := map[string]string{
			"shed.project.id":    src.ProjectID,
			"shed.project.name":  src.ProjectName,
			"shed.service.id":    src.ServiceID,
			"shed.service.name":  src.ServiceName,
			"shed.service.kind":  src.ServiceKind,
			"shed.deployment.id": src.DeploymentID,
			"shed.source":        cmpOr(src.Origin, "otlp"),
			"service.name":       serviceName,
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	edge := app
	edge.Origin = "edge"
	tests := []struct {
		name string
		src  otlp.Source
		res  *resourcepb.Resource
		want map[string]string
	}{
		{
			name: "sender values overwritten, service.name kept",
			src:  app,
			res: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				str("service.name", "checkout"),
				str("shed.service.id", "spoofed"),
				str("shed.project.id", "spoofed"),
				str("shed.source", "shed"),
				str("deployment.environment", "prod"),
			}},
			want: stamped(app, "checkout", map[string]string{"deployment.environment": "prod"}),
		},
		{
			name: "service.name defaults to the service",
			src:  app,
			res:  &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("host.name", "abc")}},
			want: stamped(app, "api", map[string]string{"host.name": "abc"}),
		},
		{
			name: "empty service.name replaced",
			src:  app,
			res:  &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", "")}},
			want: stamped(app, "api", nil),
		},
		{
			name: "no resource",
			src:  app,
			want: stamped(app, "api", nil),
		},
		{
			name: "origin",
			src:  edge,
			want: stamped(edge, "api", nil),
		},
	}
	for signal, sc := range signalCases {
		for _, tt := range tests {
			t.Run(signal+"/"+tt.name, func(t *testing.T) {
				f := newFixture(t, func(c *otlp.Config) {
					c.Attribute = func(*http.Request) (otlp.Attribution, error) { return otlp.Attribution{Source: tt.src}, nil }
				})
				var res *resourcepb.Resource
				if tt.res != nil {
					res = proto.CloneOf(tt.res)
				}
				if w := f.post(sc.path, sc.build(res)); w.Code != 200 {
					t.Fatalf("status = %d: %s", w.Code, w.Body)
				}
				rs := sc.resources(t, f)
				if len(rs) != 1 {
					t.Fatalf("forwarded %d resources, want 1", len(rs))
				}
				got := attrMap(t, rs[0].GetAttributes())
				if fmt.Sprint(got) != fmt.Sprint(tt.want) {
					t.Errorf("resource attributes\n got %v\nwant %v", got, tt.want)
				}
			})
		}
	}
}

func TestMasking(t *testing.T) {
	const secret, masked = "hunter2", "[masked]"
	var maskedFor []otlp.Source
	configure := func(c *otlp.Config) {
		c.Mask = func(src otlp.Source) func(string) string {
			maskedFor = append(maskedFor, src)
			return func(s string) string { return strings.ReplaceAll(s, secret, masked) }
		}
	}
	nested := func() *commonpb.AnyValue {
		return kvlist(str("db", "postgres://u:hunter2@db"), &commonpb.KeyValue{Key: "args", Value: array(strValue("--password=hunter2"))})
	}
	resource := func() *resourcepb.Resource {
		return &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("token", secret)}}
	}
	scope := func() *commonpb.InstrumentationScope {
		return &commonpb.InstrumentationScope{Name: "s", Attributes: []*commonpb.KeyValue{str("token", secret)}}
	}

	tests := []struct {
		name    string
		path    string
		req     proto.Message
		fwd     func(*forwarder) [][]byte
		inspect func(t *testing.T, body []byte)
	}{
		{
			name: "traces",
			path: "/v1/traces",
			req: &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
				Resource: resource(),
				ScopeSpans: []*tracepb.ScopeSpans{{Scope: scope(), Spans: []*tracepb.Span{{
					Name:       "query",
					Attributes: []*commonpb.KeyValue{{Key: "conn", Value: nested()}},
					Events:     []*tracepb.Span_Event{{Name: "exception", Attributes: []*commonpb.KeyValue{str("exception.message", "bad password hunter2")}}},
					Links:      []*tracepb.Span_Link{{Attributes: []*commonpb.KeyValue{str("why", secret)}}},
					Status:     &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR, Message: "auth failed for hunter2"},
				}}}},
			}}},
			fwd: func(f *forwarder) [][]byte { return f.traces },
			inspect: func(t *testing.T, body []byte) {
				span := unmarshal(t, body, &coltracepb.ExportTraceServiceRequest{}).ResourceSpans[0].ScopeSpans[0].Spans[0]
				args := span.Attributes[0].Value.GetKvlistValue().Values[1].Value.GetArrayValue().Values[0]
				if got := args.GetStringValue(); got != "--password="+masked {
					t.Errorf("nested array value = %q", got)
				}
				if got := span.Status.Message; got != "auth failed for "+masked {
					t.Errorf("status message = %q", got)
				}
			},
		},
		{
			name: "logs",
			path: "/v1/logs",
			req: &collogspb.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
				Resource: resource(),
				ScopeLogs: []*logspb.ScopeLogs{{Scope: scope(), LogRecords: []*logspb.LogRecord{
					{Body: strValue("login with hunter2"), Attributes: []*commonpb.KeyValue{str("user", "bob:hunter2")}},
					{Body: nested()},
				}}},
			}}},
			fwd: func(f *forwarder) [][]byte { return f.logs },
			inspect: func(t *testing.T, body []byte) {
				recs := unmarshal(t, body, &collogspb.ExportLogsServiceRequest{}).ResourceLogs[0].ScopeLogs[0].LogRecords
				if got := recs[0].Body.GetStringValue(); got != "login with "+masked {
					t.Errorf("body = %q", got)
				}
				if got := recs[1].Body.GetKvlistValue().Values[0].Value.GetStringValue(); got != "postgres://u:"+masked+"@db" {
					t.Errorf("nested body value = %q", got)
				}
			},
		},
		{
			name: "metrics",
			path: "/v1/metrics",
			req: &colmetricspb.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
				Resource: resource(),
				ScopeMetrics: []*metricspb.ScopeMetrics{{Scope: scope(), Metrics: []*metricspb.Metric{{
					Name: "requests",
					Data: &metricspb.Metric_Sum{Sum: &metricspb.Sum{DataPoints: []*metricspb.NumberDataPoint{{
						Attributes: []*commonpb.KeyValue{{Key: "conn", Value: nested()}},
						Exemplars:  []*metricspb.Exemplar{{FilteredAttributes: []*commonpb.KeyValue{str("q", secret)}}},
					}}}},
				}}}},
			}}},
			fwd: func(f *forwarder) [][]byte { return f.metrics },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maskedFor = nil
			f := newFixture(t, configure)
			if w := f.post(tt.path, tt.req); w.Code != 200 {
				t.Fatalf("status = %d: %s", w.Code, w.Body)
			}
			body := tt.fwd(f.fwd)[0]
			if bytes.Contains(body, []byte(secret)) {
				t.Error("forwarded body still contains the secret")
			}
			if c := bytes.Count(body, []byte(masked)); c < 5 {
				t.Errorf("forwarded body has %d masked values, want at least 5", c)
			}
			if len(maskedFor) == 0 || maskedFor[0] != app {
				t.Errorf("Mask called with %v, want %v", maskedFor, app)
			}
			if tt.inspect != nil {
				tt.inspect(t, body)
			}
		})
	}
}

func TestRateCaps(t *testing.T) {
	type step struct {
		advance      time.Duration
		send         int
		wantAccepted int
	}
	tests := []struct {
		name     string
		path     string
		caps     otlp.Caps
		build    func(n int) proto.Message
		rejected func(t *testing.T, body []byte, json bool) int64
		count    func(t *testing.T, f *fixture) int
	}{
		{
			name:  "traces",
			path:  "/v1/traces",
			caps:  otlp.Caps{SpansPerSecond: 10},
			build: func(n int) proto.Message { return nSpans(n) },
			rejected: func(t *testing.T, body []byte, json bool) int64 {
				resp := &coltracepb.ExportTraceServiceResponse{}
				decodeResponse(t, body, json, resp)
				return resp.GetPartialSuccess().GetRejectedSpans()
			},
			count: func(t *testing.T, f *fixture) int {
				n := 0
				for _, b := range f.fwd.traces {
					n += spanCount(unmarshal(t, b, &coltracepb.ExportTraceServiceRequest{}))
				}
				return n
			},
		},
		{
			name:  "logs",
			path:  "/v1/logs",
			caps:  otlp.Caps{LogRecordsPerSecond: 10},
			build: func(n int) proto.Message { return nLogs(n) },
			rejected: func(t *testing.T, body []byte, json bool) int64 {
				resp := &collogspb.ExportLogsServiceResponse{}
				decodeResponse(t, body, json, resp)
				return resp.GetPartialSuccess().GetRejectedLogRecords()
			},
			count: func(t *testing.T, f *fixture) int {
				n := 0
				for _, b := range f.fwd.logs {
					for _, rl := range unmarshal(t, b, &collogspb.ExportLogsServiceRequest{}).ResourceLogs {
						for _, sl := range rl.ScopeLogs {
							n += len(sl.LogRecords)
						}
					}
				}
				return n
			},
		},
	}
	steps := []step{
		{send: 15, wantAccepted: 10},
		{send: 5, wantAccepted: 0},
		{advance: 500 * time.Millisecond, send: 8, wantAccepted: 5},
		{advance: time.Hour, send: 10, wantAccepted: 10},
	}
	for _, tt := range tests {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%v", tt.name, asJSON), func(t *testing.T) {
				f := newFixture(t, func(c *otlp.Config) {
					c.Caps = func(serviceID string) otlp.Caps {
						if serviceID != app.ServiceID {
							t.Errorf("caps asked for %q", serviceID)
						}
						return tt.caps
					}
				})
				accepted, rejected := 0, 0
				for i, s := range steps {
					f.clock.advance(s.advance)
					req := request{path: tt.path, body: mustMarshal(tt.build(s.send))}
					if asJSON {
						req.contentType = jsonType
						req.body = []byte(protojson.Format(tt.build(s.send)))
					}
					w := f.do(req)
					if w.Code != 200 {
						t.Fatalf("step %d: status = %d: %s", i, w.Code, w.Body)
					}
					wantRejected := s.send - s.wantAccepted
					if got := tt.rejected(t, w.Body.Bytes(), asJSON); got != int64(wantRejected) {
						t.Errorf("step %d: rejected = %d, want %d", i, got, wantRejected)
					}
					accepted += s.wantAccepted
					rejected += wantRejected
					if got := tt.count(t, f); got != accepted {
						t.Errorf("step %d: forwarded %d records in total, want %d", i, got, accepted)
					}
				}
				if got := f.dropped(t, app.ServiceID, tt.name, "rate_cap"); got != int64(rejected) {
					t.Errorf("dropped = %d, want %d", got, rejected)
				}
				if got := f.counter(t, "shed_telemetry_ingested_total", attribute.String("signal", tt.name)); got != int64(accepted) {
					t.Errorf("ingested = %d, want %d", got, accepted)
				}
			})
		}
	}
}

func decodeResponse(t *testing.T, body []byte, json bool, m proto.Message) {
	t.Helper()
	var err error
	if json {
		err = protojson.Unmarshal(body, m)
	} else {
		err = proto.Unmarshal(body, m)
	}
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func points(t *testing.T, f *fixture) []map[string]string {
	t.Helper()
	req := unmarshal(t, f.fwd.metrics[len(f.fwd.metrics)-1], &colmetricspb.ExportMetricsServiceRequest{})
	var out []map[string]string
	for _, rm := range req.ResourceMetrics {
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				for _, p := range m.GetGauge().GetDataPoints() {
					out = append(out, attrMap(t, p.Attributes))
				}
			}
		}
	}
	return out
}

func TestSeriesCap(t *testing.T) {
	set := func(kvs ...string) []*commonpb.KeyValue {
		var out []*commonpb.KeyValue
		for i := 0; i < len(kvs); i += 2 {
			out = append(out, str(kvs[i], kvs[i+1]))
		}
		return out
	}
	route := func(r string) []*commonpb.KeyValue { return set("route", r) }
	tests := []struct {
		name         string
		advance      time.Duration
		req          *colmetricspb.ExportMetricsServiceRequest
		wantAccepted []string
	}{
		{"fills the cap", 0, gauge("http_requests", route("/a"), route("/b"), route("/c"), route("/d"), route("/e")), []string{"/a", "/b", "/c"}},
		{"known series still accepted", time.Minute, gauge("http_requests", route("/a"), route("/f"), route("/c")), []string{"/a", "/c"}},
		{"another metric name is another series", time.Minute, gauge("http_errors", route("/a")), nil},
		{"series expire after an hour", 61 * time.Minute, gauge("http_requests", route("/f"), route("/g")), []string{"/f", "/g"}},
	}
	f := newFixture(t, func(c *otlp.Config) {
		c.Caps = func(string) otlp.Caps { return otlp.Caps{Series: 3} }
	})
	dropped := int64(0)
	for _, tt := range tests {
		f.clock.advance(tt.advance)
		n := len(f.fwd.metrics)
		w := f.post("/v1/metrics", tt.req)
		if w.Code != 200 {
			t.Fatalf("%s: status = %d: %s", tt.name, w.Code, w.Body)
		}
		sent := len(tt.req.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetGauge().DataPoints)
		resp := &colmetricspb.ExportMetricsServiceResponse{}
		decodeResponse(t, w.Body.Bytes(), false, resp)
		wantRejected := int64(sent - len(tt.wantAccepted))
		if got := resp.GetPartialSuccess().GetRejectedDataPoints(); got != wantRejected {
			t.Errorf("%s: rejected = %d, want %d", tt.name, got, wantRejected)
		}
		dropped += wantRejected
		if len(tt.wantAccepted) == 0 {
			if len(f.fwd.metrics) != n {
				t.Errorf("%s: forwarded a request with no data points", tt.name)
			}
			continue
		}
		var got []string
		for _, p := range points(t, f) {
			got = append(got, p["route"])
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.wantAccepted) {
			t.Errorf("%s: forwarded routes %v, want %v", tt.name, got, tt.wantAccepted)
		}
	}
	if got := f.dropped(t, app.ServiceID, "metrics", "series_cap"); got != dropped {
		t.Errorf("dropped = %d, want %d", got, dropped)
	}

	t.Run("attribute order does not matter", func(t *testing.T) {
		f := newFixture(t, func(c *otlp.Config) {
			c.Caps = func(string) otlp.Caps { return otlp.Caps{Series: 1} }
		})
		w := f.post("/v1/metrics", gauge("m", set("a", "1", "b", "2"), set("b", "2", "a", "1")))
		resp := &colmetricspb.ExportMetricsServiceResponse{}
		decodeResponse(t, w.Body.Bytes(), false, resp)
		if resp.PartialSuccess != nil {
			t.Errorf("rejected %d data points of the same series", resp.PartialSuccess.RejectedDataPoints)
		}
	})
}

func TestForwardFailure(t *testing.T) {
	for signal, sc := range signalCases {
		t.Run(signal, func(t *testing.T) {
			f := newFixture(t, nil)
			f.fwd.err = errors.New("victoria: connection refused")
			w := f.post(sc.path, sc.build(nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.Code)
			}
			if got := w.Header().Get("Retry-After"); got != "5" {
				t.Errorf("Retry-After = %q, want 5", got)
			}
			if got := f.counter(t, "shed_telemetry_ingested_total", attribute.String("signal", signal)); got != 0 {
				t.Errorf("ingested = %d after a failed forward", got)
			}
		})
	}
}

func TestSpanMetrics(t *testing.T) {
	f := newFixture(t, nil)
	req := nSpans(205)
	ss := req.ResourceSpans[0].ScopeSpans[0]
	for _, s := range ss.Spans {
		s.StartTimeUnixNano = 1_000_000_000
		s.EndTimeUnixNano = 1_250_000_000
	}
	ss.Spans[0].Status = &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR}
	ss.Spans = append(ss.Spans,
		&tracepb.Span{Name: "outgoing", Kind: tracepb.Span_SPAN_KIND_CLIENT},
		&tracepb.Span{Name: "job", Kind: tracepb.Span_SPAN_KIND_CONSUMER},
	)
	if w := f.post("/v1/traces", req); w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	counts := map[string]uint64{}
	var statuses []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "shed_span_duration_seconds" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				name, _ := dp.Attributes.Value("span_name")
				status, _ := dp.Attributes.Value("status_code")
				svc, _ := dp.Attributes.Value("shed_service_id")
				if svc.AsString() != app.ServiceID {
					t.Errorf("shed_service_id = %q", svc.AsString())
				}
				counts[name.AsString()] += dp.Count
				if status.AsString() != "unset" {
					statuses = append(statuses, name.AsString()+"="+status.AsString())
				}
				if name.AsString() == "span 1" && dp.Sum != 0.25 {
					t.Errorf("span 1 duration = %v, want 0.25", dp.Sum)
				}
			}
		}
	}
	// 205 server spans and one consumer span: the first 200 names keep
	// their own label, the other 6 share "other"; the client span is not
	// recorded.
	if len(counts) != 201 {
		t.Errorf("%d span_name values, want 201", len(counts))
	}
	if counts["other"] != 6 {
		t.Errorf("other = %d, want 6", counts["other"])
	}
	if _, ok := counts["outgoing"]; ok {
		t.Error("client span recorded")
	}
	if fmt.Sprint(statuses) != "[span 0=error]" {
		t.Errorf("non-unset statuses = %v", statuses)
	}
}

func TestSpanAttribution(t *testing.T) {
	shed := otlp.Source{ServiceID: "shed", ServiceName: "shed", ServiceKind: "shed", Origin: "shed"}
	edgeApp := app
	edgeApp.Origin = "edge"
	domains := map[string]otlp.Source{"shop.example.com": edgeApp}
	f := newFixture(t, func(c *otlp.Config) {
		c.Attribute = func(*http.Request) (otlp.Attribution, error) {
			return otlp.Attribution{
				Source: shed,
				Span: func(resource, span otlp.Attrs) (otlp.Source, bool) {
					if name, _ := resource.Lookup("service.name"); name != "caddy" {
						return otlp.Source{}, false
					}
					host, _ := span.Lookup("http.host")
					src, ok := domains[host]
					return src, ok
				},
			}, nil
		}
		c.Caps = func(serviceID string) otlp.Caps {
			if serviceID == app.ServiceID {
				return otlp.Caps{SpansPerSecond: 1}
			}
			return otlp.Caps{}
		}
	})
	hostSpan := func(name, host string) *tracepb.Span {
		return &tracepb.Span{Name: name, Kind: tracepb.Span_SPAN_KIND_SERVER, Attributes: []*commonpb.KeyValue{str("http.host", host)}}
	}
	req := &coltracepb.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{
		{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", "caddy")}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Scope: &commonpb.InstrumentationScope{Name: "caddy"},
				Spans: []*tracepb.Span{
					hostSpan("shop 1", "shop.example.com"),
					hostSpan("dashboard", "shed.example.com"),
					hostSpan("shop 2", "shop.example.com"),
				},
			}},
		},
		{
			Resource:   &resourcepb.Resource{Attributes: []*commonpb.KeyValue{str("service.name", "shed")}},
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: []*tracepb.Span{hostSpan("GET /api/projects", "shop.example.com")}}},
		},
	}}
	w := f.post("/v1/traces", req)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	got := map[string][]string{}
	for _, rs := range lastTraces(t, f).ResourceSpans {
		attrs := attrMap(t, rs.Resource.Attributes)
		key := attrs["shed.service.id"] + "/" + attrs["shed.source"] + "/" + attrs["service.name"]
		for _, ss := range rs.ScopeSpans {
			if ss.Scope.GetName() != "" && ss.Scope.GetName() != "caddy" {
				t.Errorf("scope %q", ss.Scope.GetName())
			}
			for _, s := range ss.Spans {
				got[key] = append(got[key], s.Name)
			}
		}
	}
	want := map[string][]string{
		"s1/edge/caddy":   {"shop 1"},
		"shed/shed/caddy": {"dashboard"},
		"shed/shed/shed":  {"GET /api/projects"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("spans by source\n got %v\nwant %v", got, want)
	}
	if got := f.dropped(t, app.ServiceID, "traces", "rate_cap"); got != 1 {
		t.Errorf("dropped for %s = %d, want 1", app.ServiceID, got)
	}
}

func TestJSONHexIDs(t *testing.T) {
	const (
		traceID  = "5b8efff798038103d269b633813fc60c"
		spanID   = "eee19b7ec3c1b174"
		parentID = "eee19b7ec3c1b173"
		linkID   = "0102030405060708"
		start    = 1730000000123456789
	)
	tracesJSON := `{
  "resourceSpans": [{
    "resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "checkout"}}]},
    "scopeSpans": [{
      "scope": {"name": "my.library", "version": "1.0.0"},
      "spans": [{
        "traceId": "` + traceID + `",
        "spanId": "` + spanID + `",
        "parentSpanId": "` + parentID + `",
        "name": "GET /cart",
        "kind": 2,
        "startTimeUnixNano": "1730000000123456789",
        "endTimeUnixNano": 1730000000223456789,
        "attributes": [
          {"key": "http.response.status_code", "value": {"intValue": "200"}},
          {"key": "retry", "value": {"boolValue": false}}
        ],
        "events": [{"timeUnixNano": "1730000000133456789", "name": "cache miss"}],
        "links": [{"traceId": "` + traceID + `", "spanId": "` + linkID + `"}],
        "status": {"code": "STATUS_CODE_ERROR", "message": "boom"},
        "unknownFutureField": {"ignored": true}
      }, {
        "traceId": "` + traceID + `",
        "spanId": "` + linkID + `",
        "parentSpanId": "",
        "name": "db",
        "kind": "SPAN_KIND_CLIENT",
        "status": {"code": 1}
      }]
    }]
  }]
}`
	logsJSON := `{"resourceLogs":[{"scopeLogs":[{"logRecords":[{
  "timeUnixNano": "1730000000123456789",
  "severityNumber": 17,
  "severityText": "ERROR",
  "traceId": "` + traceID + `",
  "spanId": "` + spanID + `",
  "body": {"stringValue": "payment failed"}
}]}]}]}`
	metricsJSON := `{"resourceMetrics":[{"scopeMetrics":[{"metrics":[{
  "name": "http.server.request.duration",
  "unit": "s",
  "histogram": {
    "aggregationTemporality": 2,
    "dataPoints": [{
      "startTimeUnixNano": "1730000000000000000",
      "timeUnixNano": "1730000000123456789",
      "count": "3",
      "sum": 0.6,
      "bucketCounts": ["1", "2"],
      "explicitBounds": [0.25],
      "exemplars": [{"timeUnixNano": "1730000000100000000", "asDouble": 0.3, "traceId": "` + traceID + `", "spanId": "` + spanID + `"}]
    }]
  }
}]}]}]}`
	mustHex := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			panic(err)
		}
		return b
	}

	t.Run("traces", func(t *testing.T) {
		f := newFixture(t, nil)
		w := f.do(request{path: "/v1/traces", contentType: jsonType, body: []byte(tracesJSON)})
		if w.Code != 200 {
			t.Fatalf("status = %d: %s", w.Code, w.Body)
		}
		got := lastTraces(t, f).ResourceSpans[0].ScopeSpans[0].Spans
		s, db := got[0], got[1]
		if !bytes.Equal(s.TraceId, mustHex(traceID)) || !bytes.Equal(s.SpanId, mustHex(spanID)) || !bytes.Equal(s.ParentSpanId, mustHex(parentID)) {
			t.Errorf("ids = %x %x %x", s.TraceId, s.SpanId, s.ParentSpanId)
		}
		if !bytes.Equal(s.Links[0].TraceId, mustHex(traceID)) || !bytes.Equal(s.Links[0].SpanId, mustHex(linkID)) {
			t.Errorf("link ids = %x %x", s.Links[0].TraceId, s.Links[0].SpanId)
		}
		if len(db.ParentSpanId) != 0 {
			t.Errorf("empty parentSpanId decoded as %x", db.ParentSpanId)
		}
		if s.Kind != tracepb.Span_SPAN_KIND_SERVER || db.Kind != tracepb.Span_SPAN_KIND_CLIENT {
			t.Errorf("kinds = %v, %v", s.Kind, db.Kind)
		}
		if s.Status.Code != tracepb.Status_STATUS_CODE_ERROR || db.Status.Code != tracepb.Status_STATUS_CODE_OK {
			t.Errorf("status codes = %v, %v", s.Status.Code, db.Status.Code)
		}
		if s.StartTimeUnixNano != start || s.EndTimeUnixNano != start+100_000_000 {
			t.Errorf("times = %d, %d", s.StartTimeUnixNano, s.EndTimeUnixNano)
		}
		if got := s.Attributes[0].Value.GetIntValue(); got != 200 {
			t.Errorf("intValue = %d", got)
		}
	})

	t.Run("logs", func(t *testing.T) {
		f := newFixture(t, nil)
		w := f.do(request{path: "/v1/logs", contentType: jsonType, body: []byte(logsJSON)})
		if w.Code != 200 {
			t.Fatalf("status = %d: %s", w.Code, w.Body)
		}
		lr := unmarshal(t, f.fwd.logs[0], &collogspb.ExportLogsServiceRequest{}).ResourceLogs[0].ScopeLogs[0].LogRecords[0]
		if !bytes.Equal(lr.TraceId, mustHex(traceID)) || !bytes.Equal(lr.SpanId, mustHex(spanID)) {
			t.Errorf("ids = %x %x", lr.TraceId, lr.SpanId)
		}
		if lr.SeverityNumber != logspb.SeverityNumber_SEVERITY_NUMBER_ERROR || lr.TimeUnixNano != start {
			t.Errorf("severity %v, time %d", lr.SeverityNumber, lr.TimeUnixNano)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		f := newFixture(t, nil)
		w := f.do(request{path: "/v1/metrics", contentType: jsonType, body: []byte(metricsJSON)})
		if w.Code != 200 {
			t.Fatalf("status = %d: %s", w.Code, w.Body)
		}
		m := unmarshal(t, f.fwd.metrics[0], &colmetricspb.ExportMetricsServiceRequest{}).ResourceMetrics[0].ScopeMetrics[0].Metrics[0]
		dp := m.GetHistogram().DataPoints[0]
		if ex := dp.Exemplars[0]; !bytes.Equal(ex.TraceId, mustHex(traceID)) || !bytes.Equal(ex.SpanId, mustHex(spanID)) {
			t.Errorf("exemplar ids = %x %x", ex.TraceId, ex.SpanId)
		}
		if dp.Count != 3 || dp.TimeUnixNano != start || m.GetHistogram().AggregationTemporality != metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE {
			t.Errorf("data point = %v", dp)
		}
	})

	t.Run("invalid hex", func(t *testing.T) {
		f := newFixture(t, nil)
		body := strings.Replace(tracesJSON, spanID, "not-hex!", 1)
		if w := f.do(request{path: "/v1/traces", contentType: jsonType, body: []byte(body)}); w.Code != 400 {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestAdmitLogsSharesTheLogCap(t *testing.T) {
	f := newFixture(t, func(c *otlp.Config) {
		c.Caps = func(string) otlp.Caps { return otlp.Caps{LogRecordsPerSecond: 10} }
	})
	if got := f.h.AdmitLogs(context.Background(), app.ServiceID, 6); got != 6 {
		t.Fatalf("admitted %d of 6, want 6", got)
	}
	w := f.post("/v1/logs", nLogs(6))
	resp := &collogspb.ExportLogsServiceResponse{}
	decodeResponse(t, w.Body.Bytes(), false, resp)
	if got := resp.GetPartialSuccess().GetRejectedLogRecords(); got != 2 {
		t.Errorf("OTLP logs rejected %d after 6 admitted, want 2", got)
	}
	if got := f.h.AdmitLogs(context.Background(), app.ServiceID, 3); got != 0 {
		t.Errorf("admitted %d with the bucket empty, want 0", got)
	}
}
