package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/grafana"
)

const webhookBody = `{"status":"firing","alerts":[{"status":"firing","fingerprint":"fp",
"startsAt":"2026-10-30T12:00:00Z","labels":{"alertname":"x"}}]}`

func TestAlertWebhook(t *testing.T) {
	big := `{"alerts":[],"pad":"` + strings.Repeat("x", alertWebhookLimit) + `"}`
	tests := []struct {
		name       string
		method     string
		auth       string
		body       string
		tokenErr   error
		handleErr  error
		wantStatus int
		wantCalled bool
	}{
		{name: "ok", method: "POST", auth: "Bearer tok", body: webhookBody, wantStatus: 204, wantCalled: true},
		{name: "get", method: "GET", auth: "Bearer tok", wantStatus: 405},
		{name: "put", method: "PUT", auth: "Bearer tok", body: webhookBody, wantStatus: 405},
		{name: "no token", method: "POST", body: webhookBody, wantStatus: 401},
		{name: "wrong token", method: "POST", auth: "Bearer nope", body: webhookBody, wantStatus: 401},
		{name: "token of the wrong scheme", method: "POST", auth: "Basic tok", body: webhookBody, wantStatus: 401},
		{name: "bare token", method: "POST", auth: "tok", body: webhookBody, wantStatus: 401},
		{name: "bad token before bad body", method: "POST", auth: "Bearer nope", body: "{", wantStatus: 401},
		{name: "bad JSON", method: "POST", auth: "Bearer tok", body: "{", wantStatus: 400},
		{name: "too large", method: "POST", auth: "Bearer tok", body: big, wantStatus: 400},
		{name: "token store fails", method: "POST", auth: "Bearer tok", body: webhookBody, tokenErr: errors.New("db"), wantStatus: 500},
		{name: "handling fails", method: "POST", auth: "Bearer tok", body: webhookBody, handleErr: errors.New("db"), wantStatus: 500, wantCalled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called []grafana.WebhookAlert
			h := alertWebhook(
				func(context.Context) (string, error) { return "tok", tt.tokenErr },
				func(_ context.Context, alerts []grafana.WebhookAlert) error {
					called = alerts
					return tt.handleErr
				},
				slog.New(slog.DiscardHandler),
			)
			r := httptest.NewRequest(tt.method, AlertWebhookPath, strings.NewReader(tt.body))
			if tt.auth != "" {
				r.Header.Set("Authorization", tt.auth)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if (called != nil) != tt.wantCalled {
				t.Errorf("handled %+v, want called %v", called, tt.wantCalled)
			}
			if tt.wantCalled && (len(called) != 1 || called[0].Fingerprint != "fp") {
				t.Errorf("alerts = %+v", called)
			}
		})
	}
}

func TestAlertWebhookEmptyStoredToken(t *testing.T) {
	h := alertWebhook(
		func(context.Context) (string, error) { return "", nil },
		func(context.Context, []grafana.WebhookAlert) error { return nil },
		slog.New(slog.DiscardHandler),
	)
	r := httptest.NewRequest("POST", AlertWebhookPath, strings.NewReader(webhookBody))
	r.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// The webhook is on the host's loopback only: an app can't reach it from the
// listener inside its container.
func TestGatewayAlertWebhookOnlyOnLoopback(t *testing.T) {
	var called int
	g := NewGateway(GatewayConfig{
		AlertToken: func(context.Context) (string, error) { return "tok", nil },
		Alerts: func(context.Context, []grafana.WebhookAlert) error {
			called++
			return nil
		},
		Port: 4318,
		Log:  slog.New(slog.DiscardHandler),
	})
	hLn := make(chan string, 1)
	g.listen = func(string) (net.Listener, error) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			hLn <- "http://" + ln.Addr().String()
		}
		return ln, err
	}
	otlpHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	srv := g.listenLoopback(otlpHandler)
	if srv == nil {
		t.Fatal("no loopback server")
	}
	defer srv.Close()
	base := <-hLn

	post := func(path string) int {
		t.Helper()
		req, err := http.NewRequest("POST", base+path, strings.NewReader(webhookBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer tok")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if got := post(AlertWebhookPath); got != http.StatusNoContent || called != 1 {
		t.Errorf("webhook status %d, called %d", got, called)
	}
	if got := post("/v1/traces"); got != http.StatusTeapot {
		t.Errorf("OTLP path status %d, want the OTLP handler", got)
	}

	// An app listener serves the OTLP handler itself, webhook path included.
	r := httptest.NewRequest("POST", AlertWebhookPath, strings.NewReader(webhookBody))
	r.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	newServer(otlpHandler, &appListener{}).Handler.ServeHTTP(w, r)
	if w.Code != http.StatusTeapot || called != 1 {
		t.Errorf("app listener status %d, called %d", w.Code, called)
	}
}
