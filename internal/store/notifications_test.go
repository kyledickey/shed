package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNotificationChannels(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	c, err := s.CreateNotificationChannel(ctx, NotificationChannel{Name: "phone", Provider: "hark", Target: "https://hark/hooks/tok", Enabled: true})
	if err != nil || c.ID == "" {
		t.Fatalf("Create = %+v, %v", c, err)
	}
	if rows := rawRows(t, s, `SELECT target FROM notification_channels`); strings.Contains(rows[0][0], "tok") {
		t.Errorf("target stored as %q, want encrypted", rows[0][0])
	}
	c.Name, c.Target, c.Enabled = "pager", "https://hark/hooks/new", false
	if err := s.UpdateNotificationChannel(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.NotificationChannel(ctx, c.ID)
	if err != nil || got.Name != "pager" || got.Target != "https://hark/hooks/new" || got.Enabled {
		t.Errorf("NotificationChannel = %+v, %v", got, err)
	}
	if err := s.UpdateNotificationChannel(ctx, NotificationChannel{ID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}

	// Deleting a channel deletes its deliveries.
	if err := s.EnqueueNotification(ctx, NotificationDelivery{ChannelID: c.ID, Key: "k", Body: "b", NextAttemptAt: now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNotificationChannel(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if ds, err := s.NotificationDeliveries(ctx, 0); err != nil || len(ds) != 0 {
		t.Errorf("deliveries after delete = %v, %v", ds, err)
	}
	if _, err := s.NotificationChannel(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("NotificationChannel after delete = %v, want ErrNotFound", err)
	}
}

func TestNotificationDeliveries(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	c, err := s.CreateNotificationChannel(ctx, NotificationChannel{Name: "phone", Provider: "hark", Target: "t", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t0 := now()
	enqueue := func(key string, due time.Time) {
		t.Helper()
		d := NotificationDelivery{ChannelID: c.ID, Key: key, Title: "t", Body: "b", NextAttemptAt: due}
		if err := s.EnqueueNotification(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	enqueue("a", t0)
	enqueue("a", t0) // Duplicate key: ignored.
	enqueue("b", t0.Add(time.Hour))
	enqueue("c", t0)

	due, err := s.DueNotifications(ctx, t0, 10)
	if err != nil || len(due) != 2 || due[0].Key != "a" || due[1].Key != "c" || due[0].ChannelName != "phone" {
		t.Fatalf("due = %+v, %v; want a and c", due, err)
	}
	if err := s.NotificationSent(ctx, due[0].ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.NotificationRetry(ctx, due[1].ID, t0.Add(time.Minute), "boom"); err != nil {
		t.Fatal(err)
	}
	if due, _ = s.DueNotifications(ctx, t0, 10); len(due) != 0 {
		t.Errorf("due after send and retry = %+v, want none", due)
	}
	due, _ = s.DueNotifications(ctx, t0.Add(time.Minute), 10)
	if len(due) != 1 || due[0].Key != "c" || due[0].Attempts != 1 || due[0].LastError != "boom" {
		t.Fatalf("due after a minute = %+v, want c with 1 attempt", due)
	}
	if err := s.NotificationFailed(ctx, due[0].ID, "nope"); err != nil {
		t.Fatal(err)
	}

	all, err := s.NotificationDeliveries(ctx, 0)
	if err != nil || len(all) != 3 || all[0].Key != "c" || all[0].Status != DeliveryFailed || all[0].Attempts != 2 {
		t.Fatalf("deliveries = %+v, %v", all, err)
	}
	if all[2].Status != DeliverySent || all[2].SentAt == nil {
		t.Errorf("first delivery = %+v, want sent with a time", all[2])
	}
	if two, _ := s.NotificationDeliveries(ctx, 2); len(two) != 2 {
		t.Errorf("limit 2 returned %d", len(two))
	}

	// Pruning keeps pending rows, whatever their age.
	if err := s.PruneNotifications(ctx, now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	left, _ := s.NotificationDeliveries(ctx, 0)
	if len(left) != 1 || left[0].Key != "b" {
		t.Errorf("after prune: %+v, want only b", left)
	}
}

func TestNotificationRules(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if rs, err := s.NotificationRules(ctx); err != nil || len(rs) != 0 {
		t.Fatalf("NotificationRules = %v, %v, want none", rs, err)
	}
	th, secs := 5.0, 30
	for _, r := range []NotificationRule{
		{ID: "a", Enabled: false},
		{ID: "b", Enabled: true, Threshold: &th, ForSeconds: &secs},
		{ID: "a", Enabled: true, Threshold: &th}, // replaces the first
	} {
		if err := s.SetNotificationRule(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := s.NotificationRules(ctx)
	if err != nil || len(rs) != 2 {
		t.Fatalf("NotificationRules = %v, %v", rs, err)
	}
	if a := rs[0]; a.ID != "a" || !a.Enabled || a.Threshold == nil || *a.Threshold != 5 || a.ForSeconds != nil {
		t.Errorf("a = %+v", a)
	}
	if b := rs[1]; b.ID != "b" || b.ForSeconds == nil || *b.ForSeconds != 30 {
		t.Errorf("b = %+v", b)
	}
}
