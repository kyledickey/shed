package victoria

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// serve starts a fake store whose handler may inspect the request. It returns
// the base URL.
func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// form parses an x-www-form-urlencoded POST body.
func form(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", ct)
	}
	v, err := url.ParseQuery(readBody(t, r))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMetricsImportPrometheus(t *testing.T) {
	var body string
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/import/prometheus" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		body = readBody(t, r)
		w.WriteHeader(http.StatusNoContent)
	}), nil)

	lines := []string{`foo{bar="baz"} 123 1594370496905`, `foo{bar="qux"} 1`}
	if err := m.ImportPrometheus(context.Background(), lines); err != nil {
		t.Fatal(err)
	}
	if want := "foo{bar=\"baz\"} 123 1594370496905\nfoo{bar=\"qux\"} 1\n"; body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}

func TestMetricsImportEmptyIsNoop(t *testing.T) {
	m := NewMetrics(serve(t, func(http.ResponseWriter, *http.Request) {
		t.Error("request for empty import")
	}), nil)
	if err := m.ImportPrometheus(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestMetricsForwardOTLP(t *testing.T) {
	var got []byte
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/opentelemetry/v1/metrics" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", ct)
		}
		got = []byte(readBody(t, r))
	}), nil)
	if err := m.ForwardOTLP(context.Background(), []byte{0x0a, 0x00, 0xff}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []byte{0x0a, 0x00, 0xff}) {
		t.Errorf("body = %x", got)
	}
}

const instantVector = `{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"__name__": "foo_bar", "job": "a"}, "value": [1652169780, "3"]},
      {"metric": {"__name__": "foo_bar", "job": "b"}, "value": [1652169780.5, "NaN"]},
      {"metric": {}, "value": [1652169780, "+Inf"]}
    ]
  }
}`

const rangeMatrix = `{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {"metric": {"__name__": "foo_bar"}, "values": [[1652169600, "1"], [1652169660, "5.5"], [1652169720, "-Inf"]]}
    ]
  }
}`

func TestMetricsQuery(t *testing.T) {
	at := time.UnixMilli(1652169780_000).UTC()
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/query" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		f := form(t, r)
		if f.Get("query") != `foo_bar{job=~"a|b"}` || f.Get("time") != "1652169780.000" {
			t.Errorf("form = %v", f)
		}
		io.WriteString(w, instantVector)
	}), nil)

	res, err := m.Query(context.Background(), `foo_bar{job=~"a|b"}`, at)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "vector" || len(res.Series) != 3 {
		t.Fatalf("result = %+v", res)
	}
	a := res.Series[0]
	if a.Labels["job"] != "a" || len(a.Values) != 1 || !a.Values[0].Time.Equal(at) || a.Values[0].Value != 3 {
		t.Errorf("series a = %+v", a)
	}
	b := res.Series[1]
	if !b.Values[0].Time.Equal(at.Add(500*time.Millisecond)) || !math.IsNaN(b.Values[0].Value) {
		t.Errorf("series b = %+v", b)
	}
	if c := res.Series[2]; len(c.Labels) != 0 || !math.IsInf(c.Values[0].Value, 1) {
		t.Errorf("series c = %+v", c)
	}
}

func TestMetricsQueryOmitsZeroTime(t *testing.T) {
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if f := form(t, r); f.Has("time") {
			t.Errorf("time sent: %v", f)
		}
		io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
	}), nil)
	res, err := m.Query(context.Background(), "up", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Series == nil || len(res.Series) != 0 {
		t.Errorf("series = %#v, want empty non-nil", res.Series)
	}
}

func TestMetricsQueryRange(t *testing.T) {
	start := time.Unix(1652169600, 0)
	end := start.Add(2 * time.Minute)
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/query_range" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		f := form(t, r)
		want := url.Values{"query": {"foo_bar"}, "start": {"1652169600.000"}, "end": {"1652169720.000"}, "step": {"60000ms"}}
		if !reflect.DeepEqual(f, want) {
			t.Errorf("form = %v, want %v", f, want)
		}
		io.WriteString(w, rangeMatrix)
	}), nil)

	res, err := m.QueryRange(context.Background(), "foo_bar", start, end, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "matrix" || len(res.Series) != 1 {
		t.Fatalf("result = %+v", res)
	}
	v := res.Series[0].Values
	if len(v) != 3 || v[1].Value != 5.5 || !math.IsInf(v[2].Value, -1) || !v[1].Time.Equal(start.Add(time.Minute)) {
		t.Errorf("values = %+v", v)
	}
}

func TestMetricsScalar(t *testing.T) {
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"success","data":{"resultType":"scalar","result":[1652169780,"2"]}}`)
	}), nil)
	res, err := m.Query(context.Background(), "1+1", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "scalar" || len(res.Series) != 1 || res.Series[0].Values[0].Value != 2 {
		t.Errorf("result = %+v", res)
	}
}

func TestMetricsQueryErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
		code    int
	}{
		{"http error", 422, `{"status":"error","errorType":"422","error":"cannot parse query: unexpected end"}`, "cannot parse query", 422},
		{"error status in 200", 200, `{"status":"error","errorType":"bad_data","error":"boom"}`, "boom", 0},
		{"unsupported type", 200, `{"status":"success","data":{"resultType":"string","result":[1,"x"]}}`, "unsupported result type", 0},
		{"bad sample", 200, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"abc"]}]}}`, "abc", 0},
		{"not json", 200, `<html>`, "decode", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}), nil)
			_, err := m.Query(context.Background(), "up", time.Time{})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			var se *StatusError
			if tc.code != 0 && (!errors.As(err, &se) || se.Code != tc.code) {
				t.Errorf("err = %v, want StatusError %d", err, tc.code)
			}
		})
	}
}

func TestQueryTooLarge(t *testing.T) {
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, instantVector)
	}), nil, WithMaxBytes(64))
	_, err := m.Query(context.Background(), "up", time.Time{})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestStatusErrorBodyTruncated(t *testing.T) {
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, strings.Repeat("x", 5000))
	}), nil)
	err := m.Health(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 503 {
		t.Fatalf("err = %v", err)
	}
	if len(se.Body) != 512 || !strings.HasSuffix(se.Body, "...") {
		t.Errorf("body length = %d, suffix %q", len(se.Body), se.Body[len(se.Body)-3:])
	}
	if !strings.Contains(err.Error(), "metrics health") || !strings.Contains(err.Error(), "503") {
		t.Errorf("err = %v", err)
	}
}

func TestHealth(t *testing.T) {
	var paths []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		io.WriteString(w, "OK")
	})
	ctx := context.Background()
	for name, f := range map[string]func(context.Context) error{
		"metrics": NewMetrics(srv, nil).Health,
		"logs":    NewLogs(srv, nil).Health,
		"traces":  NewTraces(srv, nil).Health,
	} {
		if err := f(ctx); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(paths) != 3 {
		t.Fatalf("requests = %v", paths)
	}
	for _, p := range paths {
		if p != "GET /health" {
			t.Errorf("request = %q", p)
		}
	}
}

func TestBaseURLTrailingSlash(t *testing.T) {
	var path string
	m := NewMetrics(serve(t, func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path })+"/", nil)
	if err := m.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	if path != "/health" {
		t.Errorf("path = %q", path)
	}
}

func TestLogsInsertJSONLines(t *testing.T) {
	var body string
	l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/insert/jsonline" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/stream+json" {
			t.Errorf("Content-Type = %q", ct)
		}
		q := r.URL.Query()
		if q.Get("_stream_fields") != "shed.project.id,shed.service.id" || q.Get("_msg_field") != "message" || q.Get("_time_field") != "ts" {
			t.Errorf("query = %v", q)
		}
		body = readBody(t, r)
	}), nil)

	err := l.InsertJSONLines(context.Background(), []Record{
		{"ts": "2023-06-20T15:31:23Z", "message": "hello <world>", "shed.project.id": "p1", "shed.service.id": "s1"},
		{"ts": "0", "message": "two"},
	}, InsertOptions{StreamFields: []string{"shed.project.id", "shed.service.id"}, MsgField: "message", TimeField: "ts"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":"hello <world>","shed.project.id":"p1","shed.service.id":"s1","ts":"2023-06-20T15:31:23Z"}` + "\n" +
		`{"message":"two","ts":"0"}` + "\n"
	if body != want {
		t.Errorf("body = %q\nwant   %q", body, want)
	}
}

func TestLogsInsertDefaultsSendNoParams(t *testing.T) {
	l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
	}), nil)
	if err := l.InsertJSONLines(context.Background(), []Record{{"_msg": "x"}}, InsertOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestLogsForwardOTLP(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   string
	}{
		{"stream fields", []string{"shed.project.id", "shed.service.id", "shed.source"}, "shed.project.id,shed.service.id,shed.source"},
		{"none", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/insert/opentelemetry/v1/logs" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if ct := r.Header.Get("Content-Type"); ct != "application/x-protobuf" {
					t.Errorf("Content-Type = %q", ct)
				}
				if h := r.Header.Get("VL-Stream-Fields"); h != tc.want {
					t.Errorf("VL-Stream-Fields = %q, want %q", h, tc.want)
				}
				got = []byte(readBody(t, r))
			}), nil)
			if err := l.ForwardOTLP(context.Background(), []byte("pb"), tc.fields); err != nil {
				t.Fatal(err)
			}
			if string(got) != "pb" {
				t.Errorf("body = %q", got)
			}
		})
	}
}

const logsResponse = `{"_msg":"error: disconnect from 19.54.37.22: Auth fail [preauth]","_stream":"{}","_time":"2023-01-01T13:32:13Z"}
{"_msg":"some other error","_stream":"{shed.service.id=\"s1\"}","_time":"2023-01-01T13:32:15.5Z","level":"error"}

`

func TestLogsQuery(t *testing.T) {
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/select/logsql/query" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		want := url.Values{
			"query": {`{shed.service.id="s1"} error`},
			"start": {"2023-01-01T00:00:00Z"},
			"end":   {"2023-01-02T00:00:00Z"},
			"limit": {"50"},
		}
		if f := form(t, r); !reflect.DeepEqual(f, want) {
			t.Errorf("form = %v, want %v", f, want)
		}
		io.WriteString(w, logsResponse)
	}), nil)

	recs, err := l.Query(context.Background(), `{shed.service.id="s1"} error`, start, end, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{"_msg": "error: disconnect from 19.54.37.22: Auth fail [preauth]", "_stream": "{}", "_time": "2023-01-01T13:32:13Z"},
		{"_msg": "some other error", "_stream": `{shed.service.id="s1"}`, "_time": "2023-01-01T13:32:15.5Z", "level": "error"},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("records = %+v", recs)
	}
}

func TestLogsQueryOpenRange(t *testing.T) {
	l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if f := form(t, r); !reflect.DeepEqual(f, url.Values{"query": {"*"}}) {
			t.Errorf("form = %v", f)
		}
	}), nil)
	recs, err := l.Query(context.Background(), "*", time.Time{}, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if recs == nil || len(recs) != 0 {
		t.Errorf("records = %#v, want empty non-nil", recs)
	}
}

func TestLogsQueryErrors(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `cannot parse query: unexpected token "|"`)
		}), nil)
		_, err := l.Query(context.Background(), "|", time.Time{}, time.Time{}, 0)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != 400 || !strings.Contains(se.Body, "unexpected token") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad line", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "{not json}\n") }), nil)
		if _, err := l.Query(context.Background(), "*", time.Time{}, time.Time{}, 0); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("too large", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, logsResponse) }), nil, WithMaxBytes(50))
		if _, err := l.Query(context.Background(), "*", time.Time{}, time.Time{}, 0); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTracesForwardOTLP(t *testing.T) {
	var got string
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/insert/opentelemetry/v1/traces" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-protobuf" {
			t.Errorf("Content-Type = %q", ct)
		}
		got = readBody(t, r)
	}), nil)
	if err := tr.ForwardOTLP(context.Background(), []byte("pb")); err != nil {
		t.Fatal(err)
	}
	if got != "pb" {
		t.Errorf("body = %q", got)
	}
}

// jaegerTraceJSON is two traces in the shape VictoriaTraces answers with: tags
// carry string values for span.kind and error, and a CHILD_OF reference names
// the parent.
const jaegerTraceJSON = `{"data":[
 {"traceID":"aaaa","spans":[
  {"traceID":"aaaa","spanID":"s2","operationName":"db query","references":[{"refType":"CHILD_OF","traceID":"aaaa","spanID":"s1"}],
   "startTime":1749969952463000,"duration":15000,"tags":[{"key":"span.kind","type":"string","value":"client"},{"key":"error","type":"string","value":"true"},{"key":"db.rows","type":"int64","value":3}],"logs":[],"processID":"p2","warnings":null},
  {"traceID":"aaaa","spanID":"s1","operationName":"GET /","references":[],
   "startTime":1749969952453000,"duration":50000,"tags":[{"key":"span.kind","type":"string","value":"server"},{"key":"error","type":"string","value":"false"},{"key":"http.method","type":"string","value":"GET"},{"key":"cache","type":"bool","value":true}],"logs":[],"processID":"p1","warnings":null}
 ],"processes":{"p1":{"serviceName":"checkout","tags":[]},"p2":{"serviceName":"db","tags":[]}},"warnings":null},
 {"traceID":"bbbb","spans":[
  {"traceID":"bbbb","spanID":"t1","operationName":"orphan","references":[{"refType":"CHILD_OF","traceID":"bbbb","spanID":"gone"}],
   "startTime":1749970052453000,"duration":2000,"tags":[{"key":"error","type":"string","value":"unset"}],"logs":[],"processID":"p1","warnings":null}
 ],"processes":{"p1":{"serviceName":"cart","tags":[]}},"warnings":null}
],"total":0,"limit":0,"offset":0,"errors":null}`

func TestSearchTraces(t *testing.T) {
	start := time.UnixMicro(1749969952453000)
	end := start.Add(24 * time.Hour)
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/select/jaeger/api/traces" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		want := url.Values{
			"service":     {"checkout"},
			"operation":   {"GET /"},
			"tags":        {`{"http.method":"GET"}`},
			"minDuration": {"1000000ns"},
			"maxDuration": {"10000000000ns"},
			"start":       {"1749969952453000"},
			"end":         {"1750056352453000"},
			"limit":       {"5"},
		}
		if q := r.URL.Query(); !reflect.DeepEqual(q, want) {
			t.Errorf("query = %v, want %v", q, want)
		}
		io.WriteString(w, jaegerTraceJSON)
	}), nil)

	hits, err := tr.SearchTraces(context.Background(), SearchParams{
		Service:     "checkout",
		Operation:   "GET /",
		Tags:        map[string]string{"http.method": "GET"},
		MinDuration: time.Millisecond,
		MaxDuration: 10 * time.Second,
		Start:       start,
		End:         end,
		Limit:       5,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []TraceSummary{
		{TraceID: "bbbb", Service: "cart", Name: "orphan", Start: time.UnixMicro(1749970052453000).UTC(), Duration: 2 * time.Millisecond, Spans: 1},
		{TraceID: "aaaa", Service: "checkout", Name: "GET /", Start: start.UTC(), Duration: 50 * time.Millisecond, Spans: 2, Errors: 1},
	}
	if !reflect.DeepEqual(hits, want) {
		t.Errorf("hits = %+v\nwant   %+v", hits, want)
	}
}

func TestSearchTracesOmitsEmptyParams(t *testing.T) {
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		io.WriteString(w, `{"data":[],"errors":null}`)
	}), nil)
	hits, err := tr.SearchTraces(context.Background(), SearchParams{})
	if err != nil {
		t.Fatal(err)
	}
	if hits == nil || len(hits) != 0 {
		t.Errorf("hits = %#v", hits)
	}
}

func TestTrace(t *testing.T) {
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/select/jaeger/api/traces/aaaa" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		// The trace endpoint answers with the one trace.
		trimmed := jaegerTraceJSON[:strings.Index(jaegerTraceJSON, ",\n {\"traceID\":\"bbbb\"")] + "],\"errors\":null}"
		io.WriteString(w, trimmed)
	}), nil)

	got, err := tr.Trace(context.Background(), "aaaa")
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceID != "aaaa" || len(got.Spans) != 2 {
		t.Fatalf("trace = %+v", got)
	}
	root, child := got.Spans[0], got.Spans[1]
	wantRoot := Span{
		SpanID: "s1", Service: "checkout", Name: "GET /", Kind: "server", Status: "ok",
		Start: time.UnixMicro(1749969952453000).UTC(), Duration: 50 * time.Millisecond,
		Attributes: map[string]string{"http.method": "GET", "cache": "true"},
	}
	if !reflect.DeepEqual(root, wantRoot) {
		t.Errorf("root = %+v\nwant   %+v", root, wantRoot)
	}
	wantChild := Span{
		SpanID: "s2", ParentID: "s1", Service: "db", Name: "db query", Kind: "client", Status: "error",
		Start: time.UnixMicro(1749969952463000).UTC(), Duration: 15 * time.Millisecond,
		Attributes: map[string]string{"db.rows": "3"},
	}
	if !reflect.DeepEqual(child, wantChild) {
		t.Errorf("child = %+v\nwant   %+v", child, wantChild)
	}
}

func TestTraceNotFound(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"404", 404, `{"data":null,"errors":[{"code":404,"msg":"trace not found"}]}`},
		{"empty", 200, `{"data":[],"errors":null}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}), nil)
			if _, err := tr.Trace(context.Background(), "ffff"); !errors.Is(err, ErrTraceNotFound) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTraceIDIsPathEscaped(t *testing.T) {
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/select/jaeger/api/traces/a%2F..%2Fb" {
			t.Errorf("path = %q", r.URL.EscapedPath())
		}
		io.WriteString(w, `{"data":[]}`)
	}), nil)
	_, _ = tr.Trace(context.Background(), "a/../b")
}

func TestJaegerErrorsField(t *testing.T) {
	tr := NewTraces(serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":null,"errors":[{"code":400,"msg":"bad tags"}]}`)
	}), nil)
	_, err := tr.SearchTraces(context.Background(), SearchParams{})
	if err == nil || !strings.Contains(err.Error(), "bad tags") {
		t.Fatalf("err = %v", err)
	}
}

const metricsPage = `# HELP vm_data_size_bytes Size of data blocks stored in the database in bytes
# TYPE vm_data_size_bytes gauge
vm_data_size_bytes{type="indexdb/inmemory"} 0
vm_data_size_bytes{type="indexdb/file"} 1500
vm_data_size_bytes{type="storage/big"} 2.5e+03
vm_data_size_bytes{type="storage/small"} 100 1700000000000
vm_data_size_bytes_total 999999
vm_data_size_bytes_other{a="b"} 999999
vm_rows 5
`

func TestDataSize(t *testing.T) {
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/metrics" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			io.WriteString(w, strings.ReplaceAll(metricsPage, "vm_data_size_bytes", name))
		}
	}
	ctx := context.Background()

	got, err := NewMetrics(serve(t, page("vm_data_size_bytes")), nil).DataSize(ctx)
	if err != nil || got != 4100 {
		t.Errorf("metrics = %d, %v; want 4100", got, err)
	}
	got, err = NewLogs(serve(t, page("vl_data_size_bytes")), nil).DataSize(ctx)
	if err != nil || got != 4100 {
		t.Errorf("logs = %d, %v; want 4100", got, err)
	}
	got, err = NewTraces(serve(t, page("vt_data_size_bytes")), nil).DataSize(ctx)
	if err != nil || got != 4100 {
		t.Errorf("traces = %d, %v; want 4100", got, err)
	}
}

func TestDataSizeErrors(t *testing.T) {
	ctx := context.Background()
	t.Run("missing", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "vl_rows 1\n") }), nil)
		if _, err := l.DataSize(ctx); err == nil || !strings.Contains(err.Error(), "vl_data_size_bytes not found") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad value", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "vl_data_size_bytes{type=\"storage\"} abc\n")
		}), nil)
		if _, err := l.DataSize(ctx); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("status", func(t *testing.T) {
		l := NewLogs(serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }), nil)
		var se *StatusError
		if _, err := l.DataSize(ctx); !errors.As(err, &se) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestUnreachableStoreWrapsError(t *testing.T) {
	m := NewMetrics("http://127.0.0.1:1", nil)
	err := m.Health(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "victoria: metrics health: ") {
		t.Fatalf("err = %v", err)
	}
}
