package deploy

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

func TestTelemetryEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		on      bool
		own     map[string]string
		kind    string
		wantEnv map[string]string // "" means absent
	}{
		{
			name: "off",
			kind: "app",
			wantEnv: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "", "OTEL_SERVICE_NAME": "",
			},
		},
		{
			name: "on",
			on:   true,
			kind: "app",
			wantEnv: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
				"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
				"OTEL_SERVICE_NAME":           "web",
				"OTEL_RESOURCE_ATTRIBUTES":    "service.version=abc1234def",
			},
		},
		{
			name: "user variable wins",
			on:   true,
			kind: "app",
			own:  map[string]string{"OTEL_SERVICE_NAME": "storefront"},
			wantEnv: map[string]string{
				"OTEL_SERVICE_NAME":           "storefront",
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
			},
		},
		{
			name:    "databases get none",
			on:      true,
			kind:    "redis",
			wantEnv: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "", "OTEL_SERVICE_NAME": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := newFixture(t)
			f.d.telemetry = func(context.Context) (bool, error) { return tt.on, nil }
			f.d.otlp = "http://localhost:4318"
			if tt.kind != "app" {
				svc, err := f.st.CreateService(ctx, store.Service{ProjectID: f.svc.ProjectID, Name: "cache", Kind: tt.kind, Image: "redis:7"})
				if err != nil {
					t.Fatal(err)
				}
				f.svc = svc
			}
			if tt.own != nil {
				if err := f.st.SetVariables(ctx, f.svc.ID, tt.own); err != nil {
					t.Fatal(err)
				}
			}

			dep := f.wait(t, f.deploy(t).ID, terminal)
			if dep.Status != store.StatusActive {
				t.Fatalf("deployment %s: %s", dep.Status, dep.Error)
			}
			spec := f.lastRun(t)
			env := map[string]string{}
			for _, kv := range spec.Env {
				k, v, _ := strings.Cut(kv, "=")
				env[k] = v
			}
			for k, want := range tt.wantEnv {
				got, ok := env[k]
				if want == "" && ok {
					t.Errorf("%s = %q, want it absent", k, got)
				}
				if want != "" && got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}

			r, err := f.d.ResolveVariables(ctx, f.svc.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, listed := r.Env["OTEL_SERVICE_NAME"]
			own := slices.Contains(r.Own, "OTEL_SERVICE_NAME")
			if wantListed := tt.wantEnv["OTEL_SERVICE_NAME"] != ""; listed != wantListed || own != (tt.own != nil) {
				t.Errorf("resolved OTEL_SERVICE_NAME listed %v (own %v), want listed %v (own %v)", listed, own, wantListed, tt.own != nil)
			}
		})
	}
}

// lastRun returns the spec of the most recent container run.
func (f *fixture) lastRun(t *testing.T) docker.RunSpec {
	t.Helper()
	f.docker.mu.Lock()
	defer f.docker.mu.Unlock()
	if len(f.docker.runs) == 0 {
		t.Fatal("no container was run")
	}
	return f.docker.runs[len(f.docker.runs)-1]
}
