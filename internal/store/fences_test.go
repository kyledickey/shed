package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRestoreFences(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	running := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	stopped := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})
	if err := s.SetServiceStopped(ctx, stopped.ID, true); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		sv   Service
		want bool // WasStopped
	}{
		{name: "running", sv: running, want: false},
		{name: "stopped", sv: stopped, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := s.CreateRestoreFence(ctx, RestoreFence{
				ServiceID: tt.sv.ID, RestoreID: "r-" + tt.name, Phase: RestoreRetaining, Image: "img", VolumeIDs: []string{"v1", "v2"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if f.WasStopped != tt.want || f.CreatedAt.IsZero() {
				t.Errorf("fence = %+v, want WasStopped %v", f, tt.want)
			}
			if sv, _ := s.Service(ctx, tt.sv.ID); !sv.Stopped {
				t.Error("fenced service is not stopped")
			}
			if _, err := s.CreateRestoreFence(ctx, RestoreFence{ServiceID: tt.sv.ID, RestoreID: "x", Phase: RestoreRetaining}); !errors.Is(err, ErrConflict) {
				t.Errorf("second fence = %v, want ErrConflict", err)
			}
			if err := s.SetRestorePhase(ctx, tt.sv.ID, RestoreReplacing); err != nil {
				t.Fatal(err)
			}
			fs, err := s.RestoreFences(ctx)
			if err != nil {
				t.Fatal(err)
			}
			f.Phase = RestoreReplacing
			if len(fs) != 1 || !reflect.DeepEqual(fs[0], f) {
				t.Errorf("RestoreFences() = %+v, want [%+v]", fs, f)
			}
			if got, err := s.RestoreFence(ctx, tt.sv.ID); err != nil || !reflect.DeepEqual(got, f) {
				t.Errorf("RestoreFence() = %+v, %v, want %+v", got, err, f)
			}
			if err := s.LiftRestoreFence(ctx, tt.sv.ID); err != nil {
				t.Fatal(err)
			}
			if sv, _ := s.Service(ctx, tt.sv.ID); sv.Stopped != tt.want {
				t.Errorf("stopped after lifting = %v, want %v", sv.Stopped, tt.want)
			}
			if fs, _ := s.RestoreFences(ctx); len(fs) != 0 {
				t.Errorf("fences after lifting = %+v", fs)
			}
		})
	}

	if err := s.LiftRestoreFence(ctx, running.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("LiftRestoreFence without a fence = %v, want ErrNotFound", err)
	}
	if _, err := s.RestoreFence(ctx, running.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("RestoreFence without a fence = %v, want ErrNotFound", err)
	}
	if err := s.SetRestorePhase(ctx, running.ID, RestoreReplacing); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetRestorePhase without a fence = %v, want ErrNotFound", err)
	}
	if _, err := s.CreateRestoreFence(ctx, RestoreFence{ServiceID: "nope", Phase: RestoreRetaining}); !errors.Is(err, ErrNotFound) {
		t.Errorf("fence of an unknown service = %v, want ErrNotFound", err)
	}

	// Deleting a fence keeps the service stopped.
	if _, err := s.CreateRestoreFence(ctx, RestoreFence{ServiceID: running.ID, RestoreID: "r", Phase: RestoreReplacing}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRestoreFence(ctx, running.ID); err != nil {
		t.Fatal(err)
	}
	if sv, _ := s.Service(ctx, running.ID); !sv.Stopped {
		t.Error("service started by deleting its fence")
	}
	if err := s.DeleteRestoreFence(ctx, running.ID); err != nil {
		t.Errorf("deleting a missing fence = %v", err)
	}
}
