package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// auditEvents returns the events of GET /api/audit?query.
func (f *fixture) auditEvents(query string) []map[string]any {
	f.t.Helper()
	page := f.decode(f.do("GET", "/api/audit?"+query, ""), http.StatusOK)
	var out []map[string]any
	for _, e := range page["events"].([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

func TestAuditAPI(t *testing.T) {
	f := newFixture(t)
	for _, name := range []string{"a", "b", "c"} {
		f.decode(f.do("POST", "/api/projects", `{"name":"`+name+`"}`), http.StatusCreated)
	}
	f.do("POST", "/api/projects", `{"name":""}`)

	events := f.auditEvents("")
	if len(events) != 4 {
		t.Fatalf("events = %v, want 4", events)
	}
	e := events[1]
	if e["action"] != "project.create" || e["outcome"] != "ok" || e["actorKind"] != "user" || e["actor"] != "octocat" ||
		e["actorIp"] != "192.0.2.1" || e["projectId"] == "" || e["target"] != e["projectId"] || e["ts"] == nil ||
		fmt.Sprint(e["detail"]) != "map[name:c]" {
		t.Errorf("event = %v", e)
	}

	tests := []struct {
		query string
		want  int
	}{
		{"action=project", 4},
		{"action=project.create&outcome=failed", 1},
		{"actor=octocat", 4},
		{"actor=user", 4},
		{"actor=system", 0},
		{"service=nope", 0},
		{"limit=2", 2},
	}
	for _, tt := range tests {
		if got := f.auditEvents(tt.query); len(got) != tt.want {
			t.Errorf("GET /api/audit?%s = %d events, want %d", tt.query, len(got), tt.want)
		}
	}

	page := f.decode(f.do("GET", "/api/audit?limit=3", ""), http.StatusOK)
	next := page["next"].(float64)
	rest := f.decode(f.do("GET", fmt.Sprintf("/api/audit?limit=3&before=%.0f", next), ""), http.StatusOK)
	if got := rest["events"].([]any); len(got) != 1 || rest["next"] != nil {
		t.Errorf("second page = %v, want the last event and no next", rest)
	}

	for _, q := range []string{"before=x", "limit=0", "outcome=maybe"} {
		if rec := f.do("GET", "/api/audit?"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET /api/audit?%s = %d, want 400", q, rec.Code)
		}
	}

	req := httptest.NewRequest("GET", "/api/audit", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without a session = %d, want 401", rec.Code)
	}
}

func TestAuditSettingsAPI(t *testing.T) {
	f := newFixture(t)
	if got := f.decode(f.do("GET", "/api/audit/settings", ""), http.StatusOK); got["retentionDays"] != 365.0 {
		t.Errorf("default settings = %v", got)
	}
	if got := f.decode(f.do("PUT", "/api/audit/settings", `{"retentionDays":30}`), http.StatusOK); got["retentionDays"] != 30.0 {
		t.Errorf("PUT settings = %v", got)
	}
	if got := f.decode(f.do("PUT", "/api/audit/settings", `{"retentionDays":0}`), http.StatusBadRequest); !strings.Contains(fmt.Sprint(got["error"]), "retention") {
		t.Errorf("PUT 0 days = %v", got)
	}
	if got := f.decode(f.do("GET", "/api/audit/settings", ""), http.StatusOK); got["retentionDays"] != 30.0 {
		t.Errorf("settings after a rejected PUT = %v", got)
	}
	events := f.auditEvents("action=audit")
	if len(events) != 2 || events[0]["outcome"] != "failed" || events[1]["outcome"] != "ok" {
		t.Errorf("audit.settings.update events = %v, want failed then ok", events)
	}
}

func TestAuditDenials(t *testing.T) {
	f := newFixture(t)
	createApp(t, f.st)
	f.do("GET", "/api/setup/github?token=wrong", "")

	f.github.Set(newGitHubClient(t, "s3cret"))
	body := `{"ref":"refs/heads/main","after":"abc123","repository":{"full_name":"octo/app"},
		"head_commit":{"id":"abc123","message":"Fix","author":{"name":"Mona"}}}`
	send := func(signature string) {
		req := httptest.NewRequest("POST", "/api/github/webhook", strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:4000"
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", signature)
		f.handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	send("sha256=00")
	send("sha256=" + strings.Repeat("0", 64))
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(body))
	send("sha256=" + hex.EncodeToString(mac.Sum(nil)))

	events := f.auditEvents("")
	type row struct{ action, outcome, kind, ip, count string }
	var got []row
	for _, e := range events {
		d := e["detail"].(map[string]any)
		got = append(got, row{
			fmt.Sprint(e["action"]), fmt.Sprint(e["outcome"]), fmt.Sprint(e["actorKind"]), fmt.Sprint(e["actorIp"]),
			fmt.Sprint(d["count"]),
		})
	}
	want := []row{
		{"deployment.create", "ok", "github", "203.0.113.9", "<nil>"},
		{"webhook.signature.invalid", "denied", "anonymous", "203.0.113.9", "2"},
		{"setup.token.invalid", "denied", "anonymous", "192.0.2.1", "1"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("events =\n%v\nwant\n%v", got, want)
	}
}
