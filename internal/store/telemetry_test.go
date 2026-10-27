package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTelemetryCaps(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p, err := s.CreateProject(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := s.CreateService(ctx, Service{ProjectID: p.ID, Name: "web", Kind: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TelemetryCaps(ctx, svc.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("caps before set: %v, want ErrNotFound", err)
	}
	want := TelemetryCaps{SpansPerSecond: 10, LogsPerSecond: 20, Series: 30}
	if err := s.SetTelemetryCaps(ctx, svc.ID, want); err != nil {
		t.Fatal(err)
	}
	want.Series = 31
	if err := s.SetTelemetryCaps(ctx, svc.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, err := s.TelemetryCaps(ctx, svc.ID); err != nil || got != want {
		t.Fatalf("caps = %+v, %v; want %+v", got, err, want)
	}
	if all, err := s.AllTelemetryCaps(ctx); err != nil || len(all) != 1 || all[svc.ID] != want {
		t.Fatalf("all caps = %v, %v", all, err)
	}
	if err := s.DeleteService(ctx, svc.ID); err != nil {
		t.Fatal(err)
	}
	if all, err := s.AllTelemetryCaps(ctx); err != nil || len(all) != 0 {
		t.Fatalf("caps after deleting the service = %v, %v; want none", all, err)
	}
}

func TestLogCursors(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	ts := time.Date(2026, 10, 27, 12, 0, 0, 123456789, time.UTC)
	if err := s.SetLogCursor(ctx, "c1", ts.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLogCursor(ctx, "c1", ts); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLogCursor(ctx, "c2", ts); err != nil {
		t.Fatal(err)
	}
	got, err := s.LogCursors(ctx)
	if err != nil || len(got) != 2 || !got["c1"].Equal(ts) {
		t.Fatalf("cursors = %v, %v; want c1 at %v with nanoseconds", got, err, ts)
	}
	if err := s.DeleteLogCursor(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LogCursors(ctx); len(got) != 1 {
		t.Fatalf("cursors after deleting c1 = %v", got)
	}
	if err := s.DeleteLogCursors(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LogCursors(ctx); len(got) != 0 {
		t.Fatalf("cursors after deleting all = %v", got)
	}
}

func TestAuditEventsAfter(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	var ids []int64
	for range 5 {
		e, err := s.InsertAuditEvent(ctx, AuditEvent{ActorKind: "system", Action: "project.create", Outcome: "ok"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	got, err := s.AuditEventsAfter(ctx, ids[1], 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != ids[2] || got[1].ID != ids[3] {
		t.Fatalf("events after %d = %+v, want %v", ids[1], got, ids[2:4])
	}
}
