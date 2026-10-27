package audit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

func newLog(t *testing.T) (*Log, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, slog.New(slog.DiscardHandler)), st
}

func events(t *testing.T, st *store.Store) []store.AuditEvent {
	t.Helper()
	got, err := st.AuditEvents(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func detail(t *testing.T, e store.AuditEvent) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(e.Detail), &d); err != nil {
		t.Fatalf("detail %q: %v", e.Detail, err)
	}
	return d
}

func TestActorFrom(t *testing.T) {
	if got := ActorFrom(context.Background()); got != (Actor{Kind: System}) {
		t.Errorf("ActorFrom(empty) = %+v, want system", got)
	}
	a := Actor{Kind: User, Name: "octocat", IP: "203.0.113.9"}
	if got := ActorFrom(WithActor(context.Background(), a)); got != a {
		t.Errorf("ActorFrom() = %+v, want %+v", got, a)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		{"direct", "203.0.113.9:5000", nil, "203.0.113.9"},
		{"direct ignores forwarded", "203.0.113.9:5000", []string{"198.51.100.1"}, "203.0.113.9"},
		{"proxied", "127.0.0.1:5000", []string{"198.51.100.1"}, "198.51.100.1"},
		{"proxied last entry", "127.0.0.1:5000", []string{"10.0.0.1, 198.51.100.1"}, "198.51.100.1"},
		{"proxied last header", "[::1]:5000", []string{"10.0.0.1", "2001:db8::1"}, "2001:db8::1"},
		{"proxied without header", "127.0.0.1:5000", nil, "127.0.0.1"},
		{"proxied bad header", "127.0.0.1:5000", []string{"nonsense"}, "127.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := ClientIP(r); got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRecord(t *testing.T) {
	l, st := newLog(t)
	ctx := WithActor(context.Background(), Actor{Kind: Agent, Name: "octocat", ClientID: "c1", ClientName: "Claude", IP: "203.0.113.9"})
	l.Record(ctx, Event{Action: "mcp.call", Outcome: OK, ServiceID: "s1", Detail: map[string]any{"tool": "get_service"}})
	l.Record(context.Background(), Event{Action: "deployment.create", Outcome: Failed, Target: "d1"})

	got := events(t, st)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	sys, agent := got[0], got[1]
	if agent.ActorKind != "agent" || agent.Actor != "octocat" || agent.ActorIP != "203.0.113.9" ||
		agent.Action != "mcp.call" || agent.Outcome != "ok" || agent.ServiceID != "s1" {
		t.Errorf("agent event = %+v", agent)
	}
	if d := detail(t, agent); d["tool"] != "get_service" || d["client"] != "Claude" || d["clientId"] != "c1" {
		t.Errorf("agent detail = %v, want tool and client", d)
	}
	if sys.ActorKind != "system" || sys.Actor != "" || sys.Outcome != "failed" || sys.Target != "d1" || sys.Detail != "{}" {
		t.Errorf("system event = %+v", sys)
	}
}

func TestRecordCoalescesAnonymousDenials(t *testing.T) {
	l, st := newLog(t)
	now := time.Date(2026, 3, 1, 12, 0, 10, 0, time.UTC)
	l.now = func() time.Time { return now }
	from := func(kind Kind, ip string) context.Context {
		return WithActor(context.Background(), Actor{Kind: kind, IP: ip})
	}
	deny := func(ctx context.Context, action string) {
		l.Record(ctx, Event{Action: action, Outcome: Denied})
	}

	for range 3 {
		deny(from(Anonymous, "203.0.113.9"), "webhook.signature.invalid")
	}
	deny(from(Anonymous, "198.51.100.1"), "webhook.signature.invalid")
	deny(from(Anonymous, "203.0.113.9"), "setup.token.invalid")
	deny(from(User, "203.0.113.9"), "oauth.denied")
	deny(from(User, "203.0.113.9"), "oauth.denied")
	now = now.Add(time.Minute)
	deny(from(Anonymous, "203.0.113.9"), "webhook.signature.invalid")

	type row struct {
		action, ip string
		count      any
	}
	var got []row
	for _, e := range events(t, st) {
		got = append(got, row{e.Action, e.ActorIP, detail(t, e)["count"]})
	}
	want := []row{
		{"webhook.signature.invalid", "203.0.113.9", 1.0}, // the next minute
		{"oauth.denied", "203.0.113.9", nil},              // signed-in denials are not coalesced
		{"oauth.denied", "203.0.113.9", nil},
		{"setup.token.invalid", "203.0.113.9", 1.0},
		{"webhook.signature.invalid", "198.51.100.1", 1.0},
		{"webhook.signature.invalid", "203.0.113.9", 3.0},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestEventsPages(t *testing.T) {
	l, _ := newLog(t)
	ctx := context.Background()
	for range 5 {
		l.Record(ctx, Event{Action: "project.create", Outcome: OK})
	}
	first, err := l.Events(ctx, store.AuditFilter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || first.Next != first.Events[1].ID {
		t.Fatalf("first page = %+v, want 2 events and next = the last ID", first)
	}
	var seen int
	page := first
	for page.Next != 0 {
		seen += len(page.Events)
		if page, err = l.Events(ctx, store.AuditFilter{Limit: 2, Before: page.Next}); err != nil {
			t.Fatal(err)
		}
	}
	seen += len(page.Events)
	if seen != 5 {
		t.Errorf("paged through %d events, want 5", seen)
	}

	empty, err := l.Events(ctx, store.AuditFilter{Action: "nothing"})
	if err != nil || empty.Events == nil || len(empty.Events) != 0 || empty.Next != 0 {
		t.Errorf("empty page = %+v, %v, want no events and no next", empty, err)
	}
}

func TestSettingsAndPrune(t *testing.T) {
	l, st := newLog(t)
	ctx := context.Background()
	if s, err := l.Settings(ctx); err != nil || s.RetentionDays != DefaultRetentionDays {
		t.Errorf("default Settings() = %+v, %v, want %d days", s, err, DefaultRetentionDays)
	}
	for _, days := range []int{0, -1, MaxRetentionDays + 1} {
		if _, err := l.SetSettings(ctx, Settings{RetentionDays: days}); !errors.Is(err, ErrRetention) {
			t.Errorf("SetSettings(%d days) error = %v, want ErrRetention", days, err)
		}
	}
	if _, err := l.SetSettings(ctx, Settings{RetentionDays: 30}); err != nil {
		t.Fatal(err)
	}
	if s, err := l.Settings(ctx); err != nil || s.RetentionDays != 30 {
		t.Errorf("Settings() = %+v, %v, want 30 days", s, err)
	}

	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, age := range []int{31, 30, 29, 0} {
		if _, err := st.InsertAuditEvent(ctx, store.AuditEvent{
			Time: now.AddDate(0, 0, -age).Add(time.Second), ActorKind: "system", Action: "a", Outcome: "ok",
		}); err != nil {
			t.Fatal(err)
		}
	}
	l.now = func() time.Time { return now }
	if err := l.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	if got := events(t, st); len(got) != 3 {
		t.Errorf("after pruning %d events are left, want 3 (30 days old and younger)", len(got))
	}
}
