package deploy

import (
	"context"
	"errors"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/kyledickey/shed/internal/store"
)

type volumeFailureDocker struct {
	*fakeDocker
	err error
}

func (d volumeFailureDocker) RemoveVolume(context.Context, string) error { return d.err }

func TestDeleteVolumeDistinguishesFailures(t *testing.T) {
	for _, tt := range []struct {
		name      string
		err       error
		wantError bool
	}{
		{"success", nil, false},
		{"missing", cerrdefs.ErrNotFound, false},
		{"mounted", cerrdefs.ErrConflict, false},
		{"daemon unavailable", errors.New("daemon unavailable"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			v, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data")
			if err != nil {
				t.Fatal(err)
			}
			f.d.docker = volumeFailureDocker{f.docker, tt.err}
			err = f.d.DeleteVolume(context.Background(), v.ID)
			if (err != nil) != tt.wantError {
				t.Fatalf("delete error = %v", err)
			}
			_, err = f.st.Volume(context.Background(), v.ID)
			if tt.wantError && err != nil {
				t.Fatal("failed delete lost volume record")
			}
			if !tt.wantError && !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("volume remains: %v", err)
			}
		})
	}
}
