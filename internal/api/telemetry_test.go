package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/kyledickey/shed/internal/observability"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
)

func TestRequestSpansAreNamedByRoute(t *testing.T) {
	rec := telemetrytest.Install(t)
	f := newFixture(t)
	svc := createApp(t, f.st)
	if code := f.do("GET", "/api/services/"+svc.ID, "").Code; code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	req := httptest.NewRequest("GET", "/api/auth/callback?code=supersecretcode&state=x", nil)
	f.handler.ServeHTTP(httptest.NewRecorder(), req)

	s := rec.Span("GET /api/services/{id}")
	if s == nil {
		t.Fatal("no span named by the route pattern")
	}
	if s.SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v, want server", s.SpanKind())
	}
	if rec.Span("GET /api/auth/callback") == nil {
		t.Error("no span for the sign-in callback")
	}
	for _, sp := range rec.Spans() {
		if strings.Contains(sp.Name(), svc.ID) {
			t.Errorf("span name %q carries a path value", sp.Name())
		}
		for _, kv := range sp.Attributes() {
			if strings.Contains(kv.Value.String(), "supersecretcode") {
				t.Errorf("span %q attribute %s carries the OAuth code", sp.Name(), kv.Key)
			}
		}
	}
}

func TestEventStreamsAreNotTraced(t *testing.T) {
	rec := telemetrytest.Install(t)
	f := newFixture(t)
	get := func(accept string) {
		req := httptest.NewRequest("GET", "/api/logs", nil)
		req.Header.Set("Accept", accept)
		req.AddCookie(f.cookie)
		// The shed log stream ends when the request does.
		ctx, cancel := context.WithCancel(req.Context())
		cancel()
		f.handler.ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	}
	get("text/event-stream")
	if s := rec.Span("GET /api/logs"); s != nil {
		t.Error("event stream got a server span")
	}
	get("*/*")
	if s := rec.Span("GET /api/logs"); s == nil {
		t.Error("the same route without an event stream got no span")
	}
}

func TestWebhookDeliveriesAreTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Install(t)
	f := newFixture(t)
	createApp(t, f.st)
	f.github.Set(newGitHubClient(t, "s3cret"))
	body := `{"ref":"refs/heads/main","after":"abc123","repository":{"full_name":"octo/app"},
		"head_commit":{"id":"abc123","message":"Fix it","author":{"name":"Mona"}}}`
	send := func(event, signature string) {
		req := httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-Hub-Signature-256", signature)
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	good := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	send("push", good)
	send("ping", good)
	send("made-up-event", "sha256="+strings.Repeat("0", 64))

	tests := []struct{ event, outcome string }{
		{"push", "accepted"},
		{"ping", "ignored"},
		{"unverified", "unauthorized"},
	}
	for _, tt := range tests {
		n := rec.Sum(t, "shed_webhook_deliveries_total",
			attribute.String("event", tt.event), attribute.String("outcome", tt.outcome))
		if n != 1 {
			t.Errorf("shed_webhook_deliveries_total{event=%q, outcome=%q} = %d, want 1", tt.event, tt.outcome, n)
		}
	}
	if n := rec.Sum(t, "shed_webhook_deliveries_total", attribute.String("event", "made-up-event")); n != 0 {
		t.Errorf("an unverified delivery added its event as a label value")
	}

	var deliveries int
	for _, s := range rec.Spans() {
		if s.Name() != "github webhook" {
			continue
		}
		deliveries++
		if !s.Parent().IsValid() {
			t.Error("webhook span is not a child of the request span")
		}
	}
	if deliveries != 3 {
		t.Errorf("got %d webhook spans, want 3", deliveries)
	}
}

// fakeDashboards knows one dashboard, svc-logs-<service>, of every service,
// and works while f.telemetry is on.
type fakeDashboards struct {
	telemetry *fakeTelemetry
	resets    []string
	restores  int
}

func (d *fakeDashboards) Refresh() {}

func (d *fakeDashboards) List(context.Context, string) ([]observability.Dashboard, error) {
	return nil, nil
}

func (d *fakeDashboards) Reset(_ context.Context, uid string) error {
	if !d.telemetry.s.Enabled {
		return observability.ErrDisabled
	}
	if !strings.HasPrefix(uid, "svc-logs-") {
		return observability.ErrNotDashboard
	}
	d.resets = append(d.resets, uid)
	return nil
}

func (d *fakeDashboards) Restore(context.Context) error {
	if !d.telemetry.s.Enabled {
		return observability.ErrDisabled
	}
	d.restores++
	return nil
}

func (d *fakeDashboards) Service(_ context.Context, id string) (observability.ServiceDashboards, error) {
	if !d.telemetry.s.Enabled {
		return observability.ServiceDashboards{}, nil
	}
	return observability.ServiceDashboards{
		FolderURL:  "http://localhost/grafana/dashboards/f/s-" + id + "/",
		Dashboards: []observability.DashboardLink{{UID: "svc-logs-" + id, Title: "Logs", URL: "http://localhost/grafana/d/svc-logs-" + id, Customized: true, Outdated: true}},
		Alerts:     []observability.Alert{{Name: "Service restarting", ServiceID: id, Since: time.Date(2026, 10, 27, 11, 0, 0, 0, time.UTC)}},
	}, nil
}

func TestDashboardsAPI(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	reset := "/api/telemetry/dashboards/svc-logs-" + svc.ID + "/reset"

	off := f.decode(f.do("GET", "/api/services/"+svc.ID+"/telemetry", ""), http.StatusOK)
	if off["folderUrl"] != "" || len(off["dashboards"].([]any)) != 0 || len(off["firingAlerts"].([]any)) != 0 {
		t.Errorf("service telemetry while off = %v, want it empty", off)
	}
	f.decode(f.do("POST", reset, ""), http.StatusConflict)
	f.decode(f.do("POST", "/api/telemetry/dashboards/restore", ""), http.StatusConflict)

	f.telemetry.enable(true)
	got := f.decode(f.do("GET", "/api/services/"+svc.ID+"/telemetry", ""), http.StatusOK)
	want := map[string]any{
		"caps":          map[string]any{"spansPerSecond": 5000.0, "logsPerSecond": 2000.0, "series": 50000.0},
		"overridden":    false,
		"dropping":      false,
		"listenerError": "",
		"folderUrl":     "http://localhost/grafana/dashboards/f/s-" + svc.ID + "/",
		"dashboards": []any{map[string]any{
			"uid": "svc-logs-" + svc.ID, "title": "Logs", "url": "http://localhost/grafana/d/svc-logs-" + svc.ID,
			"customized": true, "outdated": true,
		}},
		"firingAlerts": []any{map[string]any{"name": "Service restarting", "since": "2026-10-27T11:00:00Z"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GET service telemetry = %v, want %v", got, want)
	}

	if rec := f.do("POST", reset, ""); rec.Code != http.StatusNoContent {
		t.Errorf("reset = %d %s", rec.Code, rec.Body)
	}
	f.decode(f.do("POST", "/api/telemetry/dashboards/someone-elses/reset", ""), http.StatusNotFound)
	if rec := f.do("POST", "/api/telemetry/dashboards/restore", ""); rec.Code != http.StatusNoContent {
		t.Errorf("restore = %d %s", rec.Code, rec.Body)
	}
	if !slices.Equal(f.dashboards.resets, []string{"svc-logs-" + svc.ID}) || f.dashboards.restores != 1 {
		t.Errorf("resets %v, restores %d", f.dashboards.resets, f.dashboards.restores)
	}

	page, err := f.st.AuditEvents(context.Background(), store.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range page {
		if strings.HasPrefix(e.Action, "telemetry.dashboard") {
			actions = append(actions, e.Action+" "+e.Outcome)
		}
	}
	slices.Reverse(actions)
	wantActions := []string{
		"telemetry.dashboard.reset failed", "telemetry.dashboards.restore failed",
		"telemetry.dashboard.reset ok", "telemetry.dashboard.reset failed", "telemetry.dashboards.restore ok",
	}
	if !slices.Equal(actions, wantActions) {
		t.Errorf("audit = %q, want %q", actions, wantActions)
	}
}

func TestServiceTelemetryRoutes(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	path := "/api/services/" + svc.ID + "/telemetry"
	for _, tt := range []struct {
		method, body string
		code         int
		want         string
	}{
		{"GET", "", http.StatusOK, `{"caps":{"spansPerSecond":5000,"logsPerSecond":2000,"series":50000},"overridden":false,"dropping":false,"listenerError":"","folderUrl":"","dashboards":[],"firingAlerts":[]}`},
		{"PUT", `{"caps":{"spansPerSecond":1,"logsPerSecond":2,"series":3}}`, http.StatusOK, `{"caps":{"spansPerSecond":1,"logsPerSecond":2,"series":3},"overridden":true,"dropping":false,"listenerError":"","folderUrl":"","dashboards":[],"firingAlerts":[]}`},
		{"PUT", `{"caps":{"spansPerSecond":0,"logsPerSecond":2,"series":3}}`, http.StatusBadRequest, ""},
		{"PUT", `{"caps":null}`, http.StatusOK, `{"caps":{"spansPerSecond":5000,"logsPerSecond":2000,"series":50000},"overridden":false,"dropping":false,"listenerError":"","folderUrl":"","dashboards":[],"firingAlerts":[]}`},
	} {
		w := f.do(tt.method, path, tt.body)
		if w.Code != tt.code {
			t.Errorf("%s %s: %d %s, want %d", tt.method, tt.body, w.Code, w.Body, tt.code)
			continue
		}
		if tt.want != "" && strings.TrimSpace(w.Body.String()) != tt.want {
			t.Errorf("%s %s: %s, want %s", tt.method, tt.body, w.Body, tt.want)
		}
	}
	if w := f.do("GET", "/api/services/nope/telemetry", ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown service: %d, want 404", w.Code)
	}
}
