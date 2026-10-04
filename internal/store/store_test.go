package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "shed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustProject(t *testing.T, s *Store, name string) Project {
	t.Helper()
	p, err := s.CreateProject(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustService(t *testing.T, s *Store, sv Service) Service {
	t.Helper()
	sv, err := s.CreateService(context.Background(), sv)
	if err != nil {
		t.Fatal(err)
	}
	return sv
}

func TestOpenReopenKeepsData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shed.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path) // Migrations must be idempotent.
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Setting(ctx, "k"); err != nil || got != "v" {
		t.Errorf("Setting() = %q, %v", got, err)
	}
}

func TestNewID(t *testing.T) {
	id := NewID()
	if len(id) != 12 || id != strings.ToLower(id) {
		t.Errorf("NewID() = %q", id)
	}
	if id == NewID() {
		t.Error("NewID() repeated")
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.Setting(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Setting(missing) error = %v, want ErrNotFound", err)
	}
	for _, v := range []string{"one", "two"} {
		if err := s.SetSetting(ctx, "k", v); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.Setting(ctx, "k"); got != "two" {
		t.Errorf("Setting() = %q, want two", got)
	}
}

func TestAddSetting(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if added, err := s.AddSetting(ctx, "k", "one"); err != nil || !added {
		t.Fatalf("first AddSetting() = %v, %v; want added", added, err)
	}
	if added, err := s.AddSetting(ctx, "k", "two"); err != nil || added {
		t.Fatalf("second AddSetting() = %v, %v; want not added", added, err)
	}
	if got, _ := s.Setting(ctx, "k"); got != "one" {
		t.Errorf("Setting() = %q, want one", got)
	}
}

func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers() = %d, %v", n, err)
	}
	if err := s.UpsertUser(ctx, User{GitHubID: 7, Login: "alice", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUser(ctx, User{GitHubID: 7, Login: "alice2", Name: "Alice B", AvatarURL: "http://a"}); err != nil {
		t.Fatal(err)
	}
	u, err := s.User(ctx, 7)
	if err != nil || u.Login != "alice2" || u.Name != "Alice B" || u.AvatarURL != "http://a" || u.CreatedAt.IsZero() {
		t.Fatalf("User() = %+v, %v", u, err)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers() = %d, want 1", n)
	}
	if _, err := s.User(ctx, 8); !errors.Is(err, ErrNotFound) {
		t.Errorf("User(8) error = %v", err)
	}

	if err := s.CreateSession(ctx, "live", 7, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, "dead", 7, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, err := s.SessionUser(ctx, "live"); err != nil || got.GitHubID != 7 {
		t.Errorf("SessionUser(live) = %+v, %v", got, err)
	}
	if _, err := s.SessionUser(ctx, "dead"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionUser(dead) error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&n); err != nil || n != 1 {
		t.Errorf("sessions after cleanup = %d, %v; want 1", n, err)
	}
	if err := s.DeleteSession(ctx, "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "live"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SessionUser after delete error = %v", err)
	}
}

func TestProjects(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "web")
	if _, err := s.CreateProject(ctx, "web"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateProject error = %v, want ErrConflict", err)
	}
	got, err := s.Project(ctx, p.ID)
	if err != nil || got.Name != "web" || got.CreatedAt.IsZero() {
		t.Fatalf("Project() = %+v, %v", got, err)
	}
	if err := s.RenameProject(ctx, p.ID, "site"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameProject(ctx, "nope", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RenameProject(nope) error = %v", err)
	}
	if ps, err := s.Projects(ctx); err != nil || len(ps) != 1 || ps[0].Name != "site" {
		t.Errorf("Projects() = %+v, %v", ps, err)
	}
	if err := s.DeleteProject(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Project(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Project after delete error = %v", err)
	}
}

func TestServices(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	app := mustService(t, s, Service{
		ProjectID: p.ID, Name: "api", Kind: "app", Repo: "Owner/Repo", Branch: "main",
		Port: 8080, AutoDeploy: true,
	})
	mustService(t, s, Service{ProjectID: p.ID, Name: "manual", Kind: "app", Repo: "owner/repo", Branch: "main"})
	mustService(t, s, Service{ProjectID: p.ID, Name: "other", Kind: "app", Repo: "owner/repo", Branch: "dev", AutoDeploy: true})
	mustService(t, s, Service{ProjectID: p.ID, Name: "db", Kind: "postgres", Repo: "owner/repo", Branch: "main", AutoDeploy: true})

	if _, err := s.CreateService(ctx, Service{ProjectID: p.ID, Name: "api", Kind: "app"}); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateService error = %v, want ErrConflict", err)
	}
	got, err := s.Service(ctx, app.ID)
	if err != nil || !reflect.DeepEqual(got, app) {
		t.Fatalf("Service() = %+v, %v; want %+v", got, err, app)
	}

	push, err := s.ServicesForPush(ctx, "owner/repo", "main")
	if err != nil || len(push) != 1 || push[0].ID != app.ID {
		t.Errorf("ServicesForPush() = %+v, %v; want only api", push, err)
	}

	app.Port, app.WaitForCI, app.Name = 9090, true, "api2"
	if err := s.UpdateService(ctx, app); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Service(ctx, app.ID); got.Port != 9090 || !got.WaitForCI || got.Name != "api2" {
		t.Errorf("after update: %+v", got)
	}
	if err := s.UpdateService(ctx, Service{ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateService(nope) error = %v", err)
	}

	if err := s.SetServiceStopped(ctx, app.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Service(ctx, app.ID); !got.Stopped {
		t.Error("after SetServiceStopped(true): Stopped = false")
	}
	app.Port = 7070 // UpdateService must not clear Stopped.
	if err := s.UpdateService(ctx, app); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Service(ctx, app.ID); !got.Stopped || got.Port != 7070 {
		t.Errorf("after update of stopped service: %+v", got)
	}
	if err := s.SetServiceStopped(ctx, app.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Service(ctx, app.ID); got.Stopped {
		t.Error("after SetServiceStopped(false): Stopped = true")
	}
	if err := s.SetServiceStopped(ctx, "nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetServiceStopped(nope) error = %v", err)
	}
	if svs, _ := s.Services(ctx, p.ID); len(svs) != 4 {
		t.Errorf("Services() len = %d, want 4", len(svs))
	}
	if svs, _ := s.AllServices(ctx); len(svs) != 4 {
		t.Errorf("AllServices() len = %d, want 4", len(svs))
	}
}

func TestVariables(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "app"})

	if vars, err := s.Variables(ctx, sv.ID); err != nil || len(vars) != 0 {
		t.Fatalf("Variables() = %v, %v", vars, err)
	}
	if err := s.SetVariables(ctx, sv.ID, map[string]string{"A": "1", "B": "2"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"B": "3", "C": "4"}
	if err := s.SetVariables(ctx, sv.ID, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Variables(ctx, sv.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Variables() = %v, want %v", got, want)
	}
	// A failed replace must leave the previous variables intact.
	if err := s.SetVariables(ctx, "missing-service", map[string]string{"X": "1"}); err == nil {
		t.Error("SetVariables on unknown service succeeded")
	}
	if got, _ := s.Variables(ctx, sv.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Variables() after failure = %v, want %v", got, want)
	}
}

func TestVolumesAndDomains(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	b := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})

	v, err := s.CreateVolume(ctx, a.ID, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateVolume(ctx, a.ID, "/data"); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate CreateVolume error = %v", err)
	}
	if _, err := s.CreateVolume(ctx, b.ID, "/data"); err != nil {
		t.Errorf("same path on another service: %v", err)
	}
	if got, err := s.Volume(ctx, v.ID); err != nil || got.MountPath != "/data" {
		t.Errorf("Volume() = %+v, %v", got, err)
	}
	if vs, _ := s.Volumes(ctx, a.ID); len(vs) != 1 {
		t.Errorf("Volumes() len = %d", len(vs))
	}
	if err := s.DeleteVolume(ctx, v.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVolume(ctx, v.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteVolume error = %v", err)
	}

	d, err := s.CreateDomain(ctx, a.ID, "App.Example.COM", true)
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "app.example.com" || !d.Generated {
		t.Errorf("CreateDomain() = %+v", d)
	}
	if _, err := s.CreateDomain(ctx, b.ID, "APP.example.com", false); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate host error = %v, want ErrConflict", err)
	}
	if got, err := s.Domain(ctx, d.ID); err != nil || !got.Generated {
		t.Errorf("Domain() = %+v, %v", got, err)
	}
	if ds, _ := s.AllDomains(ctx); len(ds) != 1 {
		t.Errorf("AllDomains() len = %d", len(ds))
	}
	if ds, _ := s.Domains(ctx, b.ID); len(ds) != 0 {
		t.Errorf("Domains(b) len = %d", len(ds))
	}
	if err := s.DeleteDomain(ctx, d.ID); err != nil {
		t.Fatal(err)
	}

	// Deleting the service cascades to its remaining resources.
	if _, err := s.CreateDomain(ctx, a.ID, "x.example.com", false); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteService(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if ds, _ := s.AllDomains(ctx); len(ds) != 0 {
		t.Errorf("domains survived service delete: %+v", ds)
	}
}

func TestPruneDeployments(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	sv := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	other := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})

	// Oldest first: active, building, then failed ones, then two newest.
	statuses := []DeploymentStatus{StatusActive, StatusBuilding, StatusCrashed, StatusFailed, StatusRemoved,
		StatusCanceled, StatusSkipped, StatusFailed, StatusRemoved}
	var ids []string
	for _, st := range statuses {
		d, err := s.CreateDeployment(ctx, Deployment{ServiceID: sv.ID, Status: st, Trigger: TriggerPush})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	if _, err := s.CreateDeployment(ctx, Deployment{ServiceID: other.ID, Status: StatusFailed, Trigger: TriggerPush}); err != nil {
		t.Fatal(err)
	}

	pruned, err := s.PruneDeployments(ctx, sv.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(pruned)
	want := slices.Sorted(slices.Values(ids[3:7]))
	if !slices.Equal(pruned, want) {
		t.Errorf("pruned %v, want %v", pruned, want)
	}
	ds, _ := s.Deployments(ctx, sv.ID, 0)
	if len(ds) != 5 {
		t.Errorf("%d deployments left, want 5", len(ds))
	}
	if ds, _ := s.Deployments(ctx, other.ID, 0); len(ds) != 1 {
		t.Errorf("other service's deployments pruned")
	}
	if pruned, err := s.PruneDeployments(ctx, sv.ID, 2); err != nil || len(pruned) != 0 {
		t.Errorf("second prune = %v, %v", pruned, err)
	}
}

func TestDeployments(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "app"})

	if _, err := s.LatestDeployment(ctx, sv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestDeployment on empty error = %v", err)
	}
	first, err := s.CreateDeployment(ctx, Deployment{ServiceID: sv.ID, Status: StatusQueued, Trigger: TriggerCreate})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateDeployment(ctx, Deployment{
		ServiceID: sv.ID, Status: StatusQueued, Trigger: TriggerPush,
		CommitSHA: "abc", CommitMessage: "msg", CommitAuthor: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	first.Status, first.Image, first.StartedAt, first.FinishedAt = StatusActive, "shed/x:1", &now, &now
	if err := s.UpdateDeployment(ctx, first); err != nil {
		t.Fatal(err)
	}
	got, err := s.Deployment(ctx, first.ID)
	if err != nil || got.Status != StatusActive || got.Image != "shed/x:1" ||
		got.StartedAt == nil || !got.StartedAt.Equal(now) || got.FinishedAt == nil {
		t.Fatalf("Deployment() = %+v, %v", got, err)
	}
	if got, _ := s.Deployment(ctx, second.ID); got.StartedAt != nil || got.CommitSHA != "abc" {
		t.Errorf("second = %+v", got)
	}

	if d, err := s.ActiveDeployment(ctx, sv.ID); err != nil || d.ID != first.ID {
		t.Errorf("ActiveDeployment() = %+v, %v", d, err)
	}
	if d, err := s.LatestDeployment(ctx, sv.ID); err != nil || d.ID != second.ID {
		t.Errorf("LatestDeployment() = %+v, %v", d, err)
	}
	ds, err := s.Deployments(ctx, sv.ID, 0)
	if err != nil || len(ds) != 2 || ds[0].ID != second.ID {
		t.Errorf("Deployments() = %+v, %v; want newest first", ds, err)
	}
	if ds, _ := s.Deployments(ctx, sv.ID, 1); len(ds) != 1 {
		t.Errorf("Deployments(limit 1) len = %d", len(ds))
	}
	if ds, _ := s.DeploymentsByStatus(ctx, StatusQueued, StatusBuilding); len(ds) != 1 || ds[0].ID != second.ID {
		t.Errorf("DeploymentsByStatus() = %+v", ds)
	}
	if ds, err := s.DeploymentsByStatus(ctx); err != nil || ds != nil {
		t.Errorf("DeploymentsByStatus() with no statuses = %v, %v", ds, err)
	}
	if err := s.UpdateDeployment(ctx, Deployment{ID: "nope"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDeployment(nope) error = %v", err)
	}
}

func TestStatusTerminal(t *testing.T) {
	for st, want := range map[DeploymentStatus]bool{
		StatusQueued: false, StatusWaiting: false, StatusBuilding: false, StatusDeploying: false,
		StatusActive: true, StatusFailed: true, StatusCrashed: true,
		StatusRemoved: true, StatusCanceled: true, StatusSkipped: true,
	} {
		if got := st.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", st, got, want)
		}
	}
}

func TestMetrics(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	a := mustService(t, s, Service{ProjectID: p.ID, Name: "a", Kind: "app"})
	b := mustService(t, s, Service{ProjectID: p.ID, Name: "b", Kind: "app"})

	from := time.Unix(1_000_000, 0)
	at := func(sec int64) time.Time { return from.Add(time.Duration(sec) * time.Second) }
	samples := []MetricSample{
		{ServiceID: a.ID, Time: at(-1), CPU: 99},                                     // before the window
		{ServiceID: a.ID, Time: at(0), CPU: 10, Memory: 100, NetRx: 1, DiskWrite: 4}, // bucket 0
		{ServiceID: a.ID, Time: at(9), CPU: 30, Memory: 300, NetRx: 3, DiskWrite: 8}, // bucket 0
		{ServiceID: a.ID, Time: at(25), CPU: 50, Memory: 500, NetTx: 2, DiskRead: 6}, // bucket 2
		{ServiceID: a.ID, Time: at(30), CPU: 99},                                     // after the window
		{ServiceID: b.ID, Time: at(5), CPU: 77},
	}
	if err := s.InsertMetricSamples(ctx, samples); err != nil {
		t.Fatal(err)
	}
	// A sample of an unknown service is skipped without failing the others.
	if err := s.InsertMetricSamples(ctx, []MetricSample{
		{ServiceID: "gone", Time: at(5), CPU: 1},
		{ServiceID: b.ID, Time: at(6), CPU: 77},
	}); err != nil {
		t.Fatalf("insert with unknown service: %v", err)
	}
	if got, _ := s.MetricBuckets(ctx, b.ID, from, 10*time.Second, 1); len(got) != 1 || got[0].CPU != 77 {
		t.Errorf("service b buckets = %+v, want one at CPU 77", got)
	}
	// Replacing a sample at the same second must not conflict.
	if err := s.InsertMetricSamples(ctx, []MetricSample{{ServiceID: a.ID, Time: at(9), CPU: 30, Memory: 300, NetRx: 3, DiskWrite: 8}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.MetricBuckets(ctx, a.ID, from, 10*time.Second, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []MetricBucket{
		{Index: 0, CPU: 20, Memory: 200, NetRx: 2, DiskWrite: 6},
		{Index: 2, CPU: 50, Memory: 500, NetTx: 2, DiskRead: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MetricBuckets() = %+v, want %+v", got, want)
	}
	if _, err := s.MetricBuckets(ctx, a.ID, from, time.Millisecond, 3); err == nil {
		t.Error("MetricBuckets(sub-second step) succeeded")
	}

	if err := s.DeleteMetricSamplesBefore(ctx, at(25)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.MetricBuckets(ctx, a.ID, from.Add(-time.Hour), time.Hour, 3)
	if want := []MetricBucket{{Index: 1, CPU: 74.5, Memory: 250, NetTx: 1, DiskRead: 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("after delete: MetricBuckets() = %+v, want %+v", got, want)
	}

	if err := s.DeleteService(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MetricBuckets(ctx, a.ID, from.Add(-time.Hour), time.Hour, 3); len(got) != 0 {
		t.Errorf("after DeleteService: MetricBuckets() = %+v, want none", got)
	}
}

func TestHostMetrics(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	from := time.Unix(1_000_000, 0)
	at := func(sec int64) time.Time { return from.Add(time.Duration(sec) * time.Second) }
	for _, m := range []HostSample{
		{Time: at(-1), CPU: 99},
		{Time: at(0), CPU: 10, Memory: 100, DiskUsed: 1000, NetRx: 1},
		{Time: at(9), CPU: 30, Memory: 300, DiskUsed: 3000, NetRx: 3},
		{Time: at(9), CPU: 30, Memory: 300, DiskUsed: 3000, NetRx: 3}, // replaces
		{Time: at(25), CPU: 50, Memory: 500, DiskUsed: 5000, DiskWrite: 6},
		{Time: at(30), CPU: 99},
	} {
		if err := s.InsertHostSample(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.HostBuckets(ctx, from, 10*time.Second, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []HostBucket{
		{Index: 0, CPU: 20, Memory: 200, DiskUsed: 2000, NetRx: 2},
		{Index: 2, CPU: 50, Memory: 500, DiskUsed: 5000, DiskWrite: 6},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HostBuckets() = %+v, want %+v", got, want)
	}
	if _, err := s.HostBuckets(ctx, from, time.Millisecond, 3); err == nil {
		t.Error("HostBuckets(sub-second step) succeeded")
	}

	if err := s.DeleteMetricSamplesBefore(ctx, at(25)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.HostBuckets(ctx, from.Add(-time.Hour), time.Hour, 3)
	if want := []HostBucket{{Index: 1, CPU: 74.5, Memory: 250, DiskUsed: 2500, DiskWrite: 3}}; !reflect.DeepEqual(got, want) {
		t.Errorf("after delete: HostBuckets() = %+v, want %+v", got, want)
	}
}

func TestDeleteDisallowedSessions(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i, login := range []string{"Alice", "bob"} {
		if err := s.UpsertUser(ctx, User{GitHubID: int64(i + 1), Login: login}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateSession(ctx, login, int64(i+1), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteDisallowedSessions(ctx, []string{"ALICE"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.DeleteDisallowedSessions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, "Alice"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
