package mcp

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/audit"
)

func TestAuditEventsTool(t *testing.T) {
	f := &fakeBackend{}
	cs := connect(t, f)
	text, isErr := call(t, cs, "audit_events", map[string]any{
		"actor": "octocat", "action": "service", "service_id": "svc1", "outcome": "ok", "since": "24h", "limit": 900,
	})
	if isErr {
		t.Fatal(text)
	}
	for _, want := range []string{
		`"action":"service.variables.update"`, `"detail":{"added":["API_TOKEN"],"service":"web"}`,
		`"actorKind":"system","id":6,`, `"outcome":"failed"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("result %s lacks %s", text, want)
		}
	}
	if strings.Contains(text, `"detail":{}`) || strings.Contains(text, `"actor":""`) {
		t.Errorf("result %s is not compact", text)
	}
	got := f.filter
	if got.Actor != "octocat" || got.Action != "service" || got.ServiceID != "svc1" || got.Outcome != "ok" ||
		got.Limit != audit.MaxLimit || time.Since(got.Since) < 23*time.Hour || time.Since(got.Since) > 25*time.Hour {
		t.Errorf("filter = %+v", got)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
		ok   bool
	}{
		{"15m", now.Add(-15 * time.Minute), true},
		{"24h", now.Add(-24 * time.Hour), true},
		{"2026-02-01T00:00:00Z", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), true},
		{"-5m", time.Time{}, false},
		{"yesterday", time.Time{}, false},
	}
	for _, tt := range tests {
		got, err := parseSince(tt.in, now)
		if (err == nil) != tt.ok || !got.Equal(tt.want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v, ok %v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestToolCallsAudited(t *testing.T) {
	f := &fakeBackend{}
	agent := audit.Actor{Kind: audit.Agent, Name: "octocat", ClientID: "c1", ClientName: "Claude"}
	cs := connectWith(t, f, func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r.WithContext(audit.WithActor(r.Context(), agent)))
		})
	})
	call(t, cs, "get_service", map[string]any{"service_id": "svc1"})
	call(t, cs, "get_service", map[string]any{"service_id": "nope"})
	call(t, cs, "list_projects", nil)

	if len(f.records) != 3 {
		t.Fatalf("recorded %d events, want 3: %+v", len(f.records), f.records)
	}
	tests := []struct {
		outcome   audit.Outcome
		serviceID string
		detail    string
	}{
		{audit.OK, "svc1", "map[arguments:map[service_id:svc1] tool:get_service]"},
		{audit.Failed, "nope", "map[arguments:map[service_id:nope] tool:get_service]"},
		{audit.OK, "", "map[tool:list_projects]"},
	}
	for i, tt := range tests {
		e := f.records[i]
		if e.Action != "mcp.call" || e.Outcome != tt.outcome || e.ServiceID != tt.serviceID || e.Actor != agent ||
			fmt.Sprint(e.Detail) != tt.detail {
			t.Errorf("event %d = %+v, want %s on %q with %s by the agent", i, e, tt.outcome, tt.serviceID, tt.detail)
		}
	}
}
