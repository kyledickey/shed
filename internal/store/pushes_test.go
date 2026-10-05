package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPendingPushes(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	b := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})

	if _, err := s.SetPendingPush(ctx, PendingPush{ServiceID: "nope", CommitSHA: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown service: err = %v, want ErrNotFound", err)
	}
	if _, err := s.PendingPush(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("no push: err = %v, want ErrNotFound", err)
	}

	old, err := s.SetPendingPush(ctx, PendingPush{ServiceID: a.ID, Repo: "o/r", Branch: "main", CommitSHA: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if old.ID == "" || old.ReceivedAt.IsZero() {
		t.Errorf("push = %+v, want ID and receipt time", old)
	}
	other, err := s.SetPendingPush(ctx, PendingPush{ServiceID: b.ID, Repo: "o/r", Branch: "dev", CommitSHA: "b1"})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.SetPendingPush(ctx, PendingPush{ServiceID: a.ID, Repo: "o/r", Branch: "main", CommitSHA: "new",
		CommitMessage: "msg", CommitAuthor: "Mona"})
	if err != nil {
		t.Fatal(err)
	}
	if newer.ID == old.ID {
		t.Error("a newer push kept the old ID")
	}
	if got, err := s.PendingPush(ctx, a.ID); err != nil || !reflect.DeepEqual(got, newer) {
		t.Errorf("PendingPush() = %+v, %v, want %+v", got, err, newer)
	}
	// Both arrived within the same millisecond, so compare without order.
	got, err := s.PendingPushes(ctx)
	if want := []PendingPush{other, newer}; err != nil || len(got) != 2 ||
		!(reflect.DeepEqual(got, want) || reflect.DeepEqual(got, []PendingPush{newer, other})) {
		t.Errorf("PendingPushes() = %+v, %v, want %+v", got, err, want)
	}

	// Deleting the replaced push leaves its successor.
	if err := s.DeletePendingPush(ctx, a.ID, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PendingPush(ctx, a.ID); err != nil {
		t.Errorf("newer push was deleted: %v", err)
	}
	if err := s.DeletePendingPush(ctx, a.ID, newer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PendingPush(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("after delete: err = %v, want ErrNotFound", err)
	}

	// Deleting the service deletes its push.
	if err := s.DeleteService(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PendingPushes(ctx); err != nil || len(got) != 0 {
		t.Errorf("after deleting the service: PendingPushes() = %+v, %v, want none", got, err)
	}
}

func TestPendingPushPriorDeployment(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	b := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})
	deploy := func(svc Service) Deployment {
		t.Helper()
		d, err := s.CreateDeployment(ctx, Deployment{ServiceID: svc.ID, Status: StatusQueued, Trigger: TriggerManual})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	push, err := s.SetPendingPush(ctx, PendingPush{ServiceID: a.ID, CommitSHA: "x"})
	if err != nil || push.PriorDeploymentID != "" {
		t.Fatalf("no deployments: push = %+v, %v, want no prior deployment", push, err)
	}
	// Created within the same millisecond, the later deployment is still the
	// prior one, and another service's deployment does not count.
	deploy(a)
	d := deploy(a)
	deploy(b)
	push, err = s.SetPendingPush(ctx, PendingPush{ServiceID: a.ID, CommitSHA: "y"})
	if err != nil || push.PriorDeploymentID != d.ID {
		t.Fatalf("push = %+v, %v, want prior deployment %s", push, err, d.ID)
	}
	if latest, err := s.LatestDeployment(ctx, a.ID); err != nil || latest.ID != push.PriorDeploymentID {
		t.Errorf("LatestDeployment() = %s, %v, want the prior deployment %s", latest.ID, err, push.PriorDeploymentID)
	}
	if got, err := s.PendingPush(ctx, a.ID); err != nil || got != push {
		t.Errorf("PendingPush() = %+v, %v, want %+v", got, err, push)
	}
}
