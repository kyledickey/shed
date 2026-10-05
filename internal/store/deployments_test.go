package store

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestActivateDeployment(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	b := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})

	create := func(sv Service) Deployment {
		t.Helper()
		d, err := s.CreateDeployment(ctx, Deployment{ServiceID: sv.ID, Status: StatusDeploying, Trigger: TriggerManual})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	old, next, other := create(a), create(a), create(b)
	for _, d := range []Deployment{old, other} {
		if err := s.ActivateDeployment(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	next.ContainerID, next.Port = "c2", 8080
	if err := s.ActivateDeployment(ctx, next); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]DeploymentStatus{old.ID: StatusRemoved, next.ID: StatusActive, other.ID: StatusActive} {
		if got, err := s.Deployment(ctx, id); err != nil || got.Status != want {
			t.Errorf("deployment %s = %s, %v; want %s", id, got.Status, err, want)
		}
	}
	if got, _ := s.Deployment(ctx, next.ID); got.ContainerID != "c2" || got.Port != 8080 {
		t.Errorf("container, port = %q, %d; want c2, 8080", got.ContainerID, got.Port)
	}
	if err := s.ActivateDeployment(ctx, Deployment{ID: "nope", ServiceID: a.ID}); !errors.Is(err, ErrNotFound) {
		t.Errorf("ActivateDeployment(nope) error = %v", err)
	}
	// A failed activation leaves the current active deployment alone.
	if got, err := s.ActiveDeployment(ctx, a.ID); err != nil || got.ID != next.ID {
		t.Errorf("ActiveDeployment() = %+v, %v", got, err)
	}

	// The schema refuses a second active deployment.
	old.Status = StatusActive
	if err := s.UpdateDeployment(ctx, old); !errors.Is(err, ErrConflict) {
		t.Errorf("second active deployment: error = %v, want ErrConflict", err)
	}
}

func TestMigrationKeepsNewestActiveDeployment(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "shed.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Build the schema as it was before the constraint.
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		version, err := migrationVersion(file)
		if err != nil {
			t.Fatal(err)
		}
		if version >= 8 {
			break
		}
		body, _ := migrationFS.ReadFile(file)
		if err := applyMigration(ctx, db, version, string(body)); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('p', 'p', '2026-01-01T00:00:00.000Z')`,
		`INSERT INTO services (id, project_id, name, kind, port, created_at) VALUES ('a', 'p', 'a', 'app', 5432, '2026-01-01T00:00:00.000Z')`,
		`INSERT INTO deployments (id, service_id, status, trigger, created_at) VALUES
			('old', 'a', 'active', 'push', '2026-01-01T00:00:01.000Z'),
			('new', 'a', 'active', 'push', '2026-01-01T00:00:02.000Z'),
			('tie', 'a', 'active', 'push', '2026-01-01T00:00:02.000Z'),
			('gone', 'a', 'removed', 'push', '2026-01-01T00:00:03.000Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	s := &Store{db: db}
	ds, err := s.DeploymentsByStatus(ctx, StatusActive)
	if err != nil {
		t.Fatal(err)
	}
	// Equal creation times are broken by insertion order, as ActiveDeployment does.
	if len(ds) != 1 || ds[0].ID != "tie" {
		t.Fatalf("active deployments = %+v, want only tie", ds)
	}
	// Deployments from before ports were recorded take the service's.
	if ds[0].Port != 5432 {
		t.Errorf("port = %d, want 5432", ds[0].Port)
	}
	for _, id := range []string{"old", "new", "gone"} {
		if d, err := s.Deployment(ctx, id); err != nil || d.Status != StatusRemoved {
			t.Errorf("deployment %s = %s, %v; want removed", id, d.Status, err)
		}
	}
}

func TestActivateDeploymentRuntime(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	create := func() Deployment {
		t.Helper()
		d, err := s.CreateDeployment(ctx, Deployment{ServiceID: a.ID, Status: StatusDeploying, Trigger: TriggerManual})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	runtime := func(id string) *DeploymentRuntime {
		t.Helper()
		d, err := s.Deployment(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return d.Runtime
	}

	old := create()
	old.Runtime = &DeploymentRuntime{
		Cmd: []string{"sh", "-c", "serve"}, Env: map[string]string{"API_SECRET": "one"},
		SecretKeys: []string{"API_SECRET"}, CPUs: 0.5, Memory: 1 << 20, PublicPort: 5432,
		Volumes: []RuntimeVolume{{ID: "v1", MountPath: "/data"}},
	}
	if err := s.ActivateDeployment(ctx, old); err != nil {
		t.Fatal(err)
	}
	got := runtime(old.ID)
	if got == nil || got.Env["API_SECRET"] != "one" || got.Volumes[0].MountPath != "/data" || got.CPUs != 0.5 || got.PublicPort != 5432 {
		t.Fatalf("runtime = %+v", got)
	}
	// Other updates keep the runtime.
	old.Status, old.ContainerID, old.Runtime = StatusActive, "c9", nil
	if err := s.UpdateDeployment(ctx, old); err != nil {
		t.Fatal(err)
	}
	if runtime(old.ID) == nil {
		t.Error("UpdateDeployment cleared the runtime")
	}
	// A superseded deployment no longer keeps resolved variables.
	next := create()
	next.Runtime = &DeploymentRuntime{Env: map[string]string{"API_SECRET": "two"}}
	if err := s.ActivateDeployment(ctx, next); err != nil {
		t.Fatal(err)
	}
	if got := runtime(old.ID); got != nil {
		t.Errorf("superseded runtime = %+v, want nil", got)
	}
	if got := runtime(next.ID); got == nil || got.Env["API_SECRET"] != "two" {
		t.Errorf("active runtime = %+v", got)
	}
	if got := runtime(create().ID); got != nil {
		t.Errorf("new deployment runtime = %+v, want nil", got)
	}
}
