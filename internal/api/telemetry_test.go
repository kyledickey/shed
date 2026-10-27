package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

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
