package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestAuditEvents(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	rows := []AuditEvent{
		{ActorKind: "user", Actor: "octocat", Action: "project.create", Outcome: "ok"},
		{ActorKind: "user", Actor: "octocat", Action: "service.create", Outcome: "ok", ServiceID: "s1"},
		{ActorKind: "agent", Actor: "octocat", Action: "mcp.call", Outcome: "ok", ServiceID: "s1"},
		{ActorKind: "system", Action: "deployment.create", Outcome: "failed", ServiceID: "s2"},
		{ActorKind: "user", Actor: "hubot", Action: "service.variables.update", Outcome: "ok", ServiceID: "s2"},
		{ActorKind: "anonymous", Action: "services.bogus", Outcome: "denied"},
	}
	var ids []int64
	for i, r := range rows {
		r.Time = base.Add(time.Duration(i) * time.Minute)
		e, err := s.InsertAuditEvent(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if e.Detail != "{}" || !e.Time.Equal(r.Time) {
			t.Errorf("inserted %+v, want detail {} and time %v", e, r.Time)
		}
		ids = append(ids, e.ID)
	}
	tests := []struct {
		name   string
		filter AuditFilter
		want   []int // indexes into rows, newest first
	}{
		{"all", AuditFilter{}, []int{5, 4, 3, 2, 1, 0}},
		{"limit", AuditFilter{Limit: 2}, []int{5, 4}},
		{"before", AuditFilter{Before: ids[3], Limit: 2}, []int{2, 1}},
		{"actor login", AuditFilter{Actor: "octocat"}, []int{2, 1, 0}},
		{"actor kind", AuditFilter{Actor: "system"}, []int{3}},
		{"action exact", AuditFilter{Action: "mcp.call"}, []int{2}},
		{"action prefix", AuditFilter{Action: "service"}, []int{4, 1}},
		{"action prefix of prefix", AuditFilter{Action: "service.variables"}, []int{4}},
		{"service", AuditFilter{ServiceID: "s2"}, []int{4, 3}},
		{"outcome", AuditFilter{Outcome: "failed"}, []int{3}},
		{"since", AuditFilter{Since: base.Add(4 * time.Minute)}, []int{5, 4}},
		{"combined", AuditFilter{Actor: "octocat", ServiceID: "s1", Action: "service"}, []int{1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.AuditEvents(ctx, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			var gotIDs, wantIDs []int64
			for _, e := range got {
				gotIDs = append(gotIDs, e.ID)
			}
			for _, i := range tt.want {
				wantIDs = append(wantIDs, ids[i])
			}
			if !slices.Equal(gotIDs, wantIDs) {
				t.Errorf("AuditEvents(%+v) IDs = %v, want %v", tt.filter, gotIDs, wantIDs)
			}
		})
	}

	if err := s.DeleteAuditEventsBefore(ctx, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := s.AuditEvents(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[len(got)-1].ID != ids[2] {
		t.Errorf("after pruning, oldest of %d events is %d, want event %d", len(got), got[len(got)-1].ID, ids[2])
	}
}

func TestCountAuditEvent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	base := time.Date(2026, 3, 1, 12, 0, 30, 0, time.UTC)
	denied := AuditEvent{ActorKind: "anonymous", Action: "webhook.signature.invalid", Outcome: "denied"}

	if ok, err := s.CountAuditEvent(ctx, denied, base); err != nil || ok {
		t.Fatalf("CountAuditEvent() on an empty log = %v, %v, want false", ok, err)
	}
	first := denied
	first.Time, first.Detail = base, `{"count":1}`
	first, err := s.InsertAuditEvent(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		event AuditEvent
		since time.Time
		want  bool
	}{
		{"same", denied, base.Truncate(time.Minute), true},
		{"again", denied, base.Truncate(time.Minute), true},
		{"other action", AuditEvent{ActorKind: "anonymous", Action: "oauth.denied", Outcome: "denied"}, base.Truncate(time.Minute), false},
		{"next minute", denied, base.Add(time.Minute).Truncate(time.Minute), false},
	}
	for _, tt := range tests {
		ok, err := s.CountAuditEvent(ctx, tt.event, tt.since)
		if err != nil || ok != tt.want {
			t.Errorf("%s: CountAuditEvent() = %v, %v, want %v", tt.name, ok, err, tt.want)
		}
	}
	got, err := s.AuditEvents(ctx, AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != first.ID || got[0].Detail != `{"count":3}` {
		t.Errorf("events = %+v, want one with count 3", got)
	}
}

func TestMigrateAuditRemovesClientAddresses(t *testing.T) {
	const addr = "198.51.100.77"
	path := filepath.Join(t.TempDir(), "shed.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	migrateTo(t, db, 22)
	if _, err := db.Exec(`INSERT INTO audit_events (ts, actor_kind, actor_ip, action, outcome)
		VALUES ('2026-10-04T03:00:00.000Z', 'anonymous', ?, 'oauth.denied', 'denied')`, addr); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events, err := s.AuditEvents(context.Background(), AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "oauth.denied" {
		t.Errorf("events after migrating = %+v, want the one row", events)
	}
	for _, f := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(addr)) {
			t.Errorf("%s still holds the address", f)
		}
	}
}
