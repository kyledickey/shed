package control

import (
	"context"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

func TestMasks(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	web := createApp(t, f.st)
	other := createApp(t, f.st)
	if err := f.st.SetVariables(ctx, web.ID, map[string]string{"TOKEN": "web-saved-secret"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, other.ID, map[string]string{"TOKEN": "other-saved-secret"}); err != nil {
		t.Fatal(err)
	}
	dep, err := f.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: web.ID, Status: store.StatusActive, Trigger: store.TriggerManual,
	})
	if err != nil {
		t.Fatal(err)
	}
	dep.Runtime = &store.DeploymentRuntime{Env: map[string]string{"TOKEN": "web-started-secret"}, SecretKeys: []string{"TOKEN"}}
	if err := f.st.ActivateDeployment(ctx, dep); err != nil {
		t.Fatal(err)
	}
	const line = "web-saved-secret web-started-secret other-saved-secret"

	telemetry, err := f.plane.TelemetryMask(ctx, web.ProjectID, dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := f.plane.RuntimeMask(ctx, dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	shed, err := f.plane.ShedMask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		mask func(string) string
		want string
	}{
		{"telemetry: project values and the container's", telemetry, "*** *** other-saved-secret"},
		{"runtime: the container's only", runtime, "web-saved-secret *** other-saved-secret"},
		{"shed: every saved value", shed, "*** web-started-secret ***"},
	} {
		if got := tt.mask(line); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}
