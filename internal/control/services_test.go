package control

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

func TestCreateService(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.plane.CreateProject(ctx, "  My Shop ")
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "My Shop" {
		t.Errorf("project name = %q, want it trimmed", project.Name)
	}

	db, err := f.plane.CreateService(ctx, project.ID, NewService{Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if db.Port != 5432 || db.Image == "" || db.Status != deploy.StatusOffline || db.LatestDeployment != nil {
		t.Errorf("database = %+v", db)
	}
	if len(db.Volumes) != 1 || db.Volumes[0].MountPath != "/var/lib/postgresql" {
		t.Errorf("volumes = %+v, want the template's", db.Volumes)
	}
	vars, err := f.plane.Variables(ctx, db.ID)
	if err != nil || vars["POSTGRES_PASSWORD"] == "" {
		t.Errorf("variables = %v, %v; want the template's", vars, err)
	}
	if db.CPULimit != defaultCPULimit || db.MemoryLimit != defaultMemoryLimit || !db.AutoDeploy {
		t.Errorf("limits = %v cores, %v bytes, auto deploy %v", db.CPULimit, db.MemoryLimit, db.AutoDeploy)
	}

	img, err := f.plane.CreateService(ctx, project.ID, NewService{Name: "img", Kind: "app", Image: " nginx:1 "})
	if err != nil || img.Image != "nginx:1" {
		t.Errorf("image app = %+v, %v; want nginx:1", img.Image, err)
	}

	_, err = f.plane.CreateService(ctx, project.ID, NewService{Name: "web", Kind: "app", Repo: "octo/app", Branch: "main"})
	wantKind(t, err, ErrConflict, "GitHub App is not configured")

	f.github = true
	web, err := f.plane.CreateService(ctx, project.ID, NewService{Name: "web", Kind: "app", Repo: "octo/app", Branch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if web.Port != defaultAppPort {
		t.Errorf("repo app port = %d, want %d", web.Port, defaultAppPort)
	}
	_, err = f.plane.CreateService(ctx, project.ID, NewService{Name: "other", Kind: "app", Repo: "octo/missing", Branch: "main"})
	wantKind(t, err, ErrInvalid, "is the GitHub App installed on it?")

	if got := f.deployer.triggers(); !slices.Equal(got, []store.Trigger{store.TriggerCreate, store.TriggerCreate, store.TriggerCreate}) {
		t.Errorf("triggers = %v, want three creates", got)
	}
	if got := f.deployer.deployed(); !slices.Equal(got, []string{"", "", "head"}) {
		t.Errorf("deployed commits = %q, want the repo app's at the head of its branch", got)
	}

	_, err = f.plane.CreateService(ctx, project.ID, NewService{Name: "db", Kind: "redis"})
	wantKind(t, err, ErrConflict, `a service named "db" already exists`)
	_, err = f.plane.CreateService(ctx, "nope", NewService{Name: "x", Kind: "redis"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("service in a missing project: error = %v, want not found", err)
	}
}

func TestCreateServiceRejects(t *testing.T) {
	f := newFixture(t)
	project, err := f.plane.CreateProject(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		req  NewService
		want string
	}{
		{"bad name", NewService{Name: "Bad_Name", Kind: "redis"}, "service name must be a DNS label"},
		{"long name", NewService{Name: strings.Repeat("a", 64), Kind: "redis"}, "service name must be a DNS label"},
		{"bad kind", NewService{Name: "x", Kind: "oracle"}, `unknown service kind "oracle"`},
		{"app without source", NewService{Name: "x", Kind: "app"}, "an app needs a repository and branch, or an image"},
		{"repo without branch", NewService{Name: "x", Kind: "app", Repo: "octo/app"}, "branch is required"},
		{"bad repo", NewService{Name: "x", Kind: "app", Repo: "octo", Branch: "main"}, "repository must be owner/name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.plane.CreateService(context.Background(), project.ID, tt.req)
			wantKind(t, err, ErrInvalid, tt.want)
		})
	}
	if got := f.deployer.triggers(); len(got) != 0 {
		t.Errorf("rejected services deployed: %v", got)
	}
}

func TestValidateService(t *testing.T) {
	app := store.Service{Kind: "app", Image: "nginx", Port: 80}
	with := func(change func(*store.Service)) store.Service {
		s := app
		change(&s)
		return s
	}
	tests := []struct {
		name string
		svc  store.Service
		want string // empty if valid
	}{
		{"valid", app, ""},
		{"database without source", store.Service{Kind: "postgres"}, ""},
		{"no source", with(func(s *store.Service) { s.Image = "" }), "an app needs a repository or an image"},
		{"repo", with(func(s *store.Service) { s.Repo, s.Branch = "octo/app", "main" }), ""},
		{"bad repo", with(func(s *store.Service) { s.Repo, s.Branch = "octo/app/x", "main" }), "repository must be owner/name"},
		{"no branch", with(func(s *store.Service) { s.Repo = "octo/app" }), "branch is required"},
		{"negative port", with(func(s *store.Service) { s.Port = -1 }), "port must be between 0 and 65535"},
		{"large port", with(func(s *store.Service) { s.Port = 65536 }), "port must be between 0 and 65535"},
		{"public port", with(func(s *store.Service) { s.PublicPort = 5432 }), ""},
		{"bad public port", with(func(s *store.Service) { s.PublicPort = 70000 }), "public port must be between 0 and 65535"},
		{"public port without port", with(func(s *store.Service) { s.Port, s.PublicPort = 0, 5432 }), "a public port needs a container port"},
		{"unlimited", with(func(s *store.Service) { s.CPULimit, s.MemoryLimit = 0, 0 }), ""},
		{"tiny CPU", with(func(s *store.Service) { s.CPULimit = 0.001 }), "CPU limit must be 0 (unlimited)"},
		{"too many CPUs", with(func(s *store.Service) { s.CPULimit = float64(runtime.NumCPU() + 1) }), "CPU limit must be 0 (unlimited)"},
		{"tiny memory", with(func(s *store.Service) { s.MemoryLimit = 1 << 20 }), "memory limit must be 0 (unlimited) or at least 64 MiB"},
		{"health check", with(func(s *store.Service) { s.HealthcheckPath = "/healthz" }), ""},
		{"relative health check", with(func(s *store.Service) { s.HealthcheckPath = "healthz" }), "health check path must start with /"},
		{"root dir", with(func(s *store.Service) { s.RootDir = "apps/web" }), ""},
		{"absolute root dir", with(func(s *store.Service) { s.RootDir = "/etc" }), "root directory must be a relative path"},
		{"escaping root dir", with(func(s *store.Service) { s.RootDir = "../x" }), "root directory must be a relative path"},
		{"Dockerfile", with(func(s *store.Service) { s.DockerfilePath = "docker/Dockerfile" }), ""},
		{"escaping Dockerfile", with(func(s *store.Service) { s.DockerfilePath = "a/../../Dockerfile" }), "Dockerfile path must be a relative path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateService(tt.svc)
			if tt.want == "" {
				if err != nil {
					t.Errorf("validateService() = %v, want valid", err)
				}
				return
			}
			wantKind(t, err, ErrInvalid, tt.want)
		})
	}
}

func TestUpdateService(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	port, cpu, root := 3000, 0.5, "../x"

	got, err := f.plane.UpdateService(ctx, svc.ID, ServicePatch{Port: &port, CPULimit: &cpu})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != port || got.CPULimit != cpu || got.Repo != svc.Repo {
		t.Errorf("updated = port %d, cpu %v, repo %q; want the patch over the rest", got.Port, got.CPULimit, got.Repo)
	}

	_, err = f.plane.UpdateService(ctx, svc.ID, ServicePatch{RootDir: &root})
	wantKind(t, err, ErrInvalid, "root directory")
	stored, err := f.st.Service(ctx, svc.ID)
	if err != nil || stored.RootDir != "" || stored.Port != port {
		t.Errorf("stored = %+v, %v; want the rejected patch not saved", stored, err)
	}
	if _, err := f.plane.UpdateService(ctx, "nope", ServicePatch{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateService(nope) = %v, want not found", err)
	}
}

func TestProjectNames(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for _, name := range []string{"", "   ", strings.Repeat("é", 65)} {
		_, err := f.plane.CreateProject(ctx, name)
		wantKind(t, err, ErrInvalid, "project name must be 1 to 64 characters")
	}
	if _, err := f.plane.CreateProject(ctx, strings.Repeat("é", 64)); err != nil {
		t.Errorf("64 characters: %v", err)
	}
	a, err := f.plane.CreateProject(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.plane.CreateProject(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	_, err = f.plane.CreateProject(ctx, "a")
	wantKind(t, err, ErrConflict, `a project named "a" already exists`)
	_, err = f.plane.RenameProject(ctx, a.ID, "b")
	wantKind(t, err, ErrConflict, `a project named "b" already exists`)
	if p, err := f.plane.RenameProject(ctx, a.ID, " c "); err != nil || p.Name != "c" {
		t.Errorf("RenameProject() = %q, %v; want c", p.Name, err)
	}
}

func TestListProjects(t *testing.T) {
	f := newFixture(t)
	svc := createApp(t, f.st)
	projects, err := f.plane.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != svc.ProjectID || len(projects[0].Services) != 1 {
		t.Fatalf("projects = %+v", projects)
	}
	if s := projects[0].Services[0]; s.ID != svc.ID || s.Name != "web" || s.Kind != "app" {
		t.Errorf("summary = %+v", s)
	}
}

func TestDeleteServicePausesBackups(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	f.deployer.onDelete = f.backups.record

	if err := f.plane.DeleteService(ctx, svc.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{"pause " + svc.ID, "delete " + svc.ID, "forget " + svc.ID, "resume " + svc.ID}
	if got := f.backups.seen(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}

	// A deletion that fails resumes backups without forgetting them.
	f.backups.calls = nil
	f.deployer.deleteErr = deploy.ErrServiceBusy
	if err := f.plane.DeleteProject(ctx, svc.ProjectID); !errors.Is(err, deploy.ErrServiceBusy) {
		t.Errorf("DeleteProject() = %v, want busy", err)
	}
	if got := f.backups.seen(); slices.Contains(got, "forget "+svc.ID) {
		t.Errorf("calls = %v, want no forget", got)
	}
}

func TestCreateDomain(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.st.CreateProject(ctx, "My Shop!")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := f.st.CreateService(ctx, store.Service{ProjectID: project.ID, Name: "web", Kind: "app", Image: "nginx"})
	if err != nil {
		t.Fatal(err)
	}

	d, pending, err := f.plane.CreateDomain(ctx, svc.ID, "")
	if err != nil || pending {
		t.Fatalf("CreateDomain(generated) = %v, pending %v", err, pending)
	}
	if d.Host != "web-my-shop.apps.example.com" || !d.Generated {
		t.Errorf("generated domain = %+v", d)
	}
	d, _, err = f.plane.CreateDomain(ctx, svc.ID, " App.Example.ORG ")
	if err != nil || d.Host != "app.example.org" || d.Generated {
		t.Errorf("custom domain = %+v, %v; want app.example.org", d, err)
	}

	tests := []struct {
		host string
		kind error
		want string
	}{
		{"app.example.org", ErrConflict, "app.example.org is already in use"},
		{"SHED.example.com", ErrConflict, "the dashboard hostname is reserved"},
		{"bad_host.example.org", ErrInvalid, "is not a valid host name"},
		{"a..b", ErrInvalid, "is not a valid host name"},
		{strings.Repeat("a.", 127) + "a", ErrInvalid, "is not a valid host name"},
	}
	for _, tt := range tests {
		_, _, err := f.plane.CreateDomain(ctx, svc.ID, tt.host)
		wantKind(t, err, tt.kind, tt.want)
	}

	f.plane.baseDomain = ""
	_, _, err = f.plane.CreateDomain(ctx, svc.ID, "")
	wantKind(t, err, ErrInvalid, "proxy.base_domain is not configured")
}

func TestGeneratedHostLength(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.st.CreateProject(ctx, strings.Repeat("long ", 20))
	if err != nil {
		t.Fatal(err)
	}
	svc := store.Service{ProjectID: project.ID, Name: "web"}
	host, err := f.plane.generatedHost(ctx, svc)
	if err != nil {
		t.Fatal(err)
	}
	label, _, _ := strings.Cut(host, ".")
	if len(label) > 63 || strings.HasSuffix(label, "-") || !validHost(host) {
		t.Errorf("generated host %q is not a valid DNS name", host)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"My Shop":     "my-shop",
		"  a--b  ":    "a-b",
		"Ünïcode 42!": "n-code-42",
		"---":         "",
	} {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateVolume(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if v, err := f.plane.CreateVolume(ctx, svc.ID, "/data"); err != nil || v.MountPath != "/data" {
		t.Fatalf("CreateVolume() = %+v, %v", v, err)
	}
	_, err := f.plane.CreateVolume(ctx, svc.ID, "/data")
	wantKind(t, err, ErrConflict, "a volume is already mounted at /data")
	for _, p := range []string{"", "/", "data", "/data/", "/a/../b"} {
		_, err := f.plane.CreateVolume(ctx, svc.ID, p)
		wantKind(t, err, ErrInvalid, "mount path must be a clean absolute path")
	}
}

func TestExplain(t *testing.T) {
	tests := []struct {
		err  error
		kind error
		msg  string
	}{
		{errorf(ErrInvalid, "bad %s", "input"), ErrInvalid, "bad input"},
		{fmt.Errorf("wrapped: %w", errorf(ErrConflict, "taken")), ErrConflict, "taken"},
		{fmt.Errorf("store: service x: %w", store.ErrNotFound), ErrNotFound, "not found"},
		{store.ErrConflict, ErrConflict, "already exists"},
		{deploy.ErrFenced, ErrConflict, msgFenced},
		{backup.ErrFenced, ErrConflict, msgFenced},
		{deploy.ErrNoContainer, ErrConflict, "nothing to run; deploy the service first"},
		{deploy.ErrStopped, ErrUnavailable, "shutting down"},
		{fmt.Errorf("%w: keep must be at least 1", backup.ErrInvalid), ErrInvalid, "keep must be at least 1"},
	}
	for _, tt := range tests {
		e, ok := Explain(tt.err)
		if !ok || e.Kind != tt.kind || !strings.HasSuffix(e.Msg, tt.msg) {
			t.Errorf("Explain(%v) = %+v, %v; want %v %q", tt.err, e, ok, tt.kind, tt.msg)
		}
	}
	if e, ok := Explain(errors.New("disk on fire")); ok {
		t.Errorf("Explain(unexpected) = %+v, want false", e)
	}
}
