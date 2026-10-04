package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type unreadBody struct{ t *testing.T }

func (b unreadBody) Read([]byte) (int, error) {
	b.t.Error("unauthenticated malformed signature read body")
	return 0, io.EOF
}
func (unreadBody) Close() error { return nil }
func TestWebhookRejectsBeforeReading(t *testing.T) {
	f := newFixture(t)
	f.github.Set(newGitHubClient(t, "secret"))
	req := httptest.NewRequest("POST", "/api/github/webhook", nil)
	req.Body = unreadBody{t}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader(strings.Repeat("x", maxWebhookBody+1)))
	req.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status %d", rec.Code)
	}
}
func TestWebhookAdmission(t *testing.T) {
	var guard webhookGuard
	now := time.Now()
	for range 4 {
		if !guard.acquire(now) {
			t.Fatal("initial capacity unavailable")
		}
	}
	if guard.acquire(now) {
		t.Fatal("concurrency limit exceeded")
	}
	for range 4 {
		guard.release()
	}
	for range 4 {
		if !guard.acquire(now) {
			t.Fatal("burst rejected")
		}
		guard.release()
	}
	if guard.acquire(now) {
		t.Fatal("rate limit exceeded")
	}
	if !guard.acquire(now.Add(time.Second)) {
		t.Fatal("token did not refill")
	}
	guard.release()
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineRecorder) SetReadDeadline(d time.Time) error {
	w.deadlines = append(w.deadlines, d)
	return nil
}
func TestWebhookReadDeadline(t *testing.T) {
	f := newFixture(t)
	f.github.Set(newGitHubClient(t, "secret"))
	req := httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader("{}"))
	req.Header.Set("X-Hub-Signature-256", "sha256="+strings.Repeat("0", 64))
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	before := time.Now()
	f.handler.ServeHTTP(w, req)
	if len(w.deadlines) != 1 || w.deadlines[0].Before(before.Add(9*time.Second)) || w.deadlines[0].After(before.Add(11*time.Second)) {
		t.Fatalf("read deadlines %v", w.deadlines)
	}
}
