package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/victoria"
)

// fakeLogStore answers log queries with records and remembers the queries.
type fakeLogStore struct {
	mu      sync.Mutex
	records []victoria.Record
	queries []string
	ends    []time.Time
}

func (f *fakeLogStore) Query(_ context.Context, q string, _, end time.Time, limit int) ([]victoria.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	f.ends = append(f.ends, end)
	return f.records[:min(limit, len(f.records))], nil
}

func TestLogHistory(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	f.telemetry.enable(true)
	f.logStore.records = []victoria.Record{
		{"_time": "2026-05-01T12:00:03Z", "_msg": "boom", "level": "ERROR", "shed.source": "container", "stream": "stderr", "shed.deployment.id": "dep1", "trace_id": "t1", "user": "u"},
		{"_time": "2026-05-01T12:00:02Z", "_msg": "ok", "level": "info", "shed.source": "otlp"},
	}
	target := "/api/services/" + svc.ID + "/logs/history?" + url.Values{
		"q": {"boom"}, "level": {"error"}, "limit": {"2"}, "from": {"2026-05-01T00:00:00Z"},
	}.Encode()
	body := f.decode(f.do("GET", target, ""), http.StatusOK)
	if body["next"] != "2026-05-01T12:00:02Z" {
		t.Errorf("next = %v", body["next"])
	}
	recs := body["records"].([]any)
	first := recs[0].(map[string]any)
	if len(recs) != 2 || first["message"] != "boom" || first["level"] != "error" || first["source"] != "container" ||
		first["stream"] != "stderr" || first["deploymentId"] != "dep1" || first["traceId"] != "t1" || first["fields"].(map[string]any)["user"] != "u" {
		t.Errorf("records = %v", recs)
	}
	if q := f.logStore.queries[0]; !strings.HasPrefix(q, `_stream:{"shed.service.id"="`+svc.ID+`"} AND "boom" AND (`) {
		t.Errorf("query = %s", q)
	}

	// The next page asks for records before the cursor.
	f.do("GET", "/api/services/"+svc.ID+"/logs/history?before=2026-05-01T12:00:02Z", "")
	if got := f.logStore.ends[1]; !got.Equal(time.Date(2026, 5, 1, 12, 0, 2, 0, time.UTC).Add(-time.Nanosecond)) {
		t.Errorf("end = %v", got)
	}

	for target, want := range map[string]int{
		"/logs/history?level=loud":      http.StatusBadRequest,
		"/logs/history?q=%22open":       http.StatusBadRequest,
		"/logs/history?from=yesterday":  http.StatusBadRequest,
		"/logs/history?before=1":        http.StatusBadRequest,
		"/logs/history?limit=0":         http.StatusBadRequest,
		"/logs/history?limit=many":      http.StatusBadRequest,
		"/logs/history?to=2026-05-01":   http.StatusBadRequest,
		"/logs/history?q=a+OR+b+%7C+x":  http.StatusOK,
		"/logs/history?q=_stream%3Ax":   http.StatusOK,
		"/logs/history?level=error&q=x": http.StatusOK,
	} {
		if code := f.do("GET", "/api/services/"+svc.ID+target, "").Code; code != want {
			t.Errorf("%s = %d, want %d", target, code, want)
		}
	}
	if code := f.do("GET", "/api/services/nope/logs/history", "").Code; code != http.StatusNotFound {
		t.Errorf("unknown service = %d", code)
	}
}

func TestLogHistoryWhileTelemetryIsOff(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	rec := f.do("GET", "/api/services/"+svc.ID+"/logs/history", "")
	if body := f.decode(rec, http.StatusConflict); body["error"] != "telemetry is off" {
		t.Errorf("body = %v", body)
	}
	if len(f.logStore.queries) != 0 {
		t.Errorf("store asked while off: %v", f.logStore.queries)
	}
}

func TestTraceRedirect(t *testing.T) {
	f := newFixture(t)
	id := "4bf92f3577b34da6a3ce929d0e0e4736"
	if code := f.do("GET", "/api/telemetry/trace/"+id, "").Code; code != http.StatusConflict {
		t.Errorf("while off = %d, want 409", code)
	}
	f.telemetry.enable(true)
	rec := f.do("GET", "/api/telemetry/trace/"+id, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d; %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Path != "/grafana/explore" || loc.Host != "" {
		t.Fatalf("Location = %q", rec.Header().Get("Location"))
	}
	if loc.Query().Get("schemaVersion") != "1" || !strings.Contains(loc.Query().Get("panes"), `"query":"`+id+`"`) ||
		!strings.Contains(loc.Query().Get("panes"), `"shed-traces"`) {
		t.Errorf("Location query = %s", loc.RawQuery)
	}
	for _, bad := range []string{"nothex", strings.ToUpper(id), id + "00", "a"} {
		if code := f.do("GET", "/api/telemetry/trace/"+bad, "").Code; code != http.StatusBadRequest {
			t.Errorf("trace %q = %d, want 400", bad, code)
		}
	}
	anon := httptest.NewRecorder()
	f.handler.ServeHTTP(anon, httptest.NewRequest("GET", "/api/telemetry/trace/"+id, nil))
	if anon.Code != http.StatusUnauthorized {
		t.Errorf("without a session = %d, want 401", anon.Code)
	}
}
