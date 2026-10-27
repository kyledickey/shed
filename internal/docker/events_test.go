package docker

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
)

func TestEventFrom(t *testing.T) {
	tests := []struct {
		name string
		in   events.Message
		want Event
		ok   bool
	}{
		{
			name: "exec_create carries the command",
			in: events.Message{
				Type: events.ContainerEventType, Action: "exec_create: /bin/sh -c echo hi", TimeNano: 5,
				Actor: events.Actor{ID: "c1", Attributes: map[string]string{"name": "web", "execID": "e1", "shed.service": "s1"}},
			},
			want: Event{Type: "container", Action: "exec_create", ID: "c1", Name: "web", ExecID: "e1", Command: "/bin/sh -c echo hi"},
			ok:   true,
		},
		{
			name: "die carries the exit code",
			in: events.Message{
				Type: events.ContainerEventType, Action: "die", TimeNano: 5,
				Actor: events.Actor{ID: "c1", Attributes: map[string]string{"name": "web", "exitCode": "137"}},
			},
			want: Event{Type: "container", Action: "die", ID: "c1", Name: "web", ExitCode: 137},
			ok:   true,
		},
		{
			name: "image pull is named by its reference",
			in:   events.Message{Type: events.ImageEventType, Action: "pull", TimeNano: 5, Actor: events.Actor{ID: "alpine:3"}},
			want: Event{Type: "image", Action: "pull", ID: "alpine:3", Name: "alpine:3"},
			ok:   true,
		},
		{name: "other container actions", in: events.Message{Type: events.ContainerEventType, Action: "exec_start: ls"}},
		{name: "health status", in: events.Message{Type: events.ContainerEventType, Action: "health_status: healthy"}},
		{name: "other image actions", in: events.Message{Type: events.ImageEventType, Action: "tag"}},
		{name: "other types", in: events.Message{Type: events.NetworkEventType, Action: "connect"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := eventFrom(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			got.Time, got.Attributes = time.Time{}, nil
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("event = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHealthCommand(t *testing.T) {
	tests := []struct {
		test []string
		want string
	}{
		{nil, ""},
		{[]string{"NONE"}, ""},
		{[]string{"CMD-SHELL", "pg_isready -U postgres"}, "/bin/sh -c pg_isready -U postgres"},
		{[]string{"CMD", "redis-cli", "ping"}, "redis-cli ping"},
	}
	for _, tc := range tests {
		if got := healthCommand(&container.HealthConfig{Test: tc.test}); got != tc.want {
			t.Errorf("healthCommand(%v) = %q, want %q", tc.test, got, tc.want)
		}
	}
	if got := healthCommand(nil); got != "" {
		t.Errorf("healthCommand(nil) = %q", got)
	}
}

func TestExecSetExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &execSet{now: func() time.Time { return now }}
	if s.has("a") {
		t.Fatal("empty set has a")
	}
	s.add("a")
	if !s.has("a") {
		t.Fatal("a not remembered")
	}
	now = now.Add(ownExecTTL - time.Second)
	if !s.has("a") {
		t.Error("a forgotten before its TTL")
	}
	now = now.Add(2 * time.Second)
	if s.has("a") {
		t.Error("a remembered past its TTL")
	}
}

func TestExecSetIsBounded(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &execSet{now: func() time.Time { return now }}
	for i := range ownExecMax + 10 {
		now = now.Add(time.Millisecond)
		s.add(fmt.Sprint(i))
	}
	if len(s.ids) != ownExecMax {
		t.Fatalf("len = %d, want %d", len(s.ids), ownExecMax)
	}
	if s.has("0") {
		t.Error("oldest ID kept")
	}
	if !s.has(fmt.Sprint(ownExecMax + 9)) {
		t.Error("newest ID dropped")
	}

	now = now.Add(ownExecTTL + time.Second)
	s.add("fresh")
	if len(s.ids) != 1 {
		t.Errorf("expired IDs kept: len = %d", len(s.ids))
	}
}

func TestExecSetConcurrent(t *testing.T) {
	var s execSet
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 400 {
				id := fmt.Sprint(i, "-", j)
				s.add(id)
				if !s.has(id) {
					t.Errorf("%s forgotten", id)
				}
			}
		})
	}
	wg.Wait()
}
