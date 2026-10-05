package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
)

// fakeDocker keeps containers in memory.
type fakeDocker struct {
	mu             sync.Mutex
	next           int
	containers     map[string]*docker.Container
	runs           []docker.RunSpec
	restarts       []string
	aliases        map[string][]string // network aliases by container ID
	exposed        []int               // ports every image exposes
	output         string              // what every container prints
	exitCode       int                 // if set, new containers exit at once with it
	removedVolumes []string
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{containers: make(map[string]*docker.Container), aliases: make(map[string][]string)}
}

// ReconnectNetwork replaces the container's aliases, keeping its address.
func (f *fakeDocker) ReconnectNetwork(_ context.Context, network, id string, aliases []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return errNoContainer(id)
	}
	if _, ok := c.IPs[network]; !ok {
		return fmt.Errorf("container %s is not connected to %s", id, network)
	}
	f.aliases[id] = aliases
	return nil
}

func (f *fakeDocker) DisconnectNetwork(_ context.Context, network, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return errNoContainer(id)
	}
	if _, ok := c.IPs[network]; !ok {
		return fmt.Errorf("container %s is not connected to %s", id, network)
	}
	delete(c.IPs, network)
	delete(f.aliases, id)
	return nil
}

// resolve returns the containers that the alias name resolves to.
func (f *fakeDocker) resolve(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, aliases := range f.aliases {
		if slices.Contains(aliases, name) {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

func (f *fakeDocker) EnsureNetwork(context.Context, string) error        { return nil }
func (f *fakeDocker) RemoveNetwork(context.Context, string) error        { return nil }
func (f *fakeDocker) EnsureVolume(context.Context, string) error         { return nil }
func (f *fakeDocker) PullImage(context.Context, string, io.Writer) error { return nil }

func (f *fakeDocker) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removedVolumes = append(f.removedVolumes, name)
	return nil
}
func (f *fakeDocker) ResolveImage(context.Context, string) (string, error) {
	return "sha256:original", nil
}
func (f *fakeDocker) ExposedPorts(context.Context, string) ([]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exposed, nil
}
func (f *fakeDocker) RemoveImage(context.Context, string) error                  { return nil }
func (f *fakeDocker) ListImages(context.Context, string) ([]docker.Image, error) { return nil, nil }

// Logs writes the output, then, if following, blocks until the container
// stops or ctx ends.
func (f *fakeDocker) Logs(ctx context.Context, id string, _ int, follow bool, w io.Writer) error {
	f.mu.Lock()
	out := f.output
	f.mu.Unlock()
	io.WriteString(w, out)
	for follow {
		if c, ok := f.container(id); !ok || !c.Running {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

func (f *fakeDocker) Run(_ context.Context, spec docker.RunSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("c%d", f.next)
	f.containers[id] = &docker.Container{
		ID: id, Name: spec.Name, Image: spec.Image, State: "running", Running: true,
		IPs:    map[string]string{spec.Network: fmt.Sprintf("10.0.0.%d", f.next)},
		Labels: spec.Labels,
	}
	f.aliases[id] = spec.Aliases
	if f.exitCode != 0 {
		c := f.containers[id]
		c.State, c.Running, c.ExitCode = "exited", false, f.exitCode
	}
	f.runs = append(f.runs, spec)
	return id, nil
}

func (f *fakeDocker) Start(_ context.Context, id string) error {
	return f.setRunning(id, true)
}

func (f *fakeDocker) Stop(_ context.Context, id string, _ time.Duration) error {
	return f.setRunning(id, false)
}

func (f *fakeDocker) Restart(_ context.Context, id string, _ time.Duration) error {
	f.mu.Lock()
	f.restarts = append(f.restarts, id)
	f.mu.Unlock()
	return f.setRunning(id, true)
}

func (f *fakeDocker) setRunning(id string, running bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return errNoContainer(id)
	}
	c.Running, c.State = running, "running"
	if !running {
		c.State = "exited"
	}
	return nil
}

func (f *fakeDocker) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.containers, id)
	delete(f.aliases, id)
	return nil
}

func (f *fakeDocker) Inspect(_ context.Context, id string) (docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return docker.Container{}, errNoContainer(id)
	}
	return *c, nil
}

// errNoContainer is the error Docker returns for a missing container.
func errNoContainer(id string) error {
	return fmt.Errorf("no such container %s: %w", id, cerrdefs.ErrNotFound)
}

func (f *fakeDocker) List(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []docker.Container
next:
	for _, c := range f.containers {
		for k, v := range labels {
			if c.Labels[k] != v {
				continue next
			}
		}
		out = append(out, *c)
	}
	return out, nil
}

func (f *fakeDocker) container(id string) (docker.Container, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return docker.Container{}, false
	}
	return *c, true
}

// fakeBuilder succeeds, unless block is set, in which case it signals blocked
// and waits for cancellation.
type fakeBuilder struct {
	block   atomic.Bool
	blocked chan struct{}
}

func (b *fakeBuilder) Build(ctx context.Context, _ string, req build.Request, out io.Writer) error {
	fmt.Fprintf(out, "building %s\n", req.Image)
	if b.block.Load() {
		b.blocked <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

type fakeGitHub struct{}

func (fakeGitHub) CloneURL(context.Context, string) (string, error) {
	return "https://example.com/repo.git", nil
}

func (fakeGitHub) CIStatus(context.Context, string, string) (github.CIState, error) {
	return github.CISuccess, nil
}

type fakeProxy struct {
	mu     sync.Mutex
	routes []proxy.Route
}

func (p *fakeProxy) Apply(routes []proxy.Route) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes = routes
	return nil
}

type fixture struct {
	d       *Deployer
	st      *store.Store
	docker  *fakeDocker
	builder *fakeBuilder
	proxy   *fakeProxy
	svc     store.Service
	healthy func() bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "shed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	f := &fixture{st: st, docker: newFakeDocker(), builder: &fakeBuilder{blocked: make(chan struct{}, 1)}, proxy: &fakeProxy{}}
	f.healthy = func() bool { return true }
	f.d = New(Config{
		Store:   st,
		Docker:  f.docker,
		Builder: f.builder,
		Proxy:   f.proxy,
		GitHub:  func() (GitHub, bool) { return fakeGitHub{}, true },
		LogDir:  dir,
		Log:     slog.New(slog.DiscardHandler),
	})
	f.d.healthInterval = 10 * time.Millisecond
	f.d.healthTimeout = 200 * time.Millisecond
	f.d.logPoll = 10 * time.Millisecond
	f.d.startupWatch = 50 * time.Millisecond
	f.d.logDrain = time.Second
	f.d.probe = func(context.Context, string, string) error {
		if f.healthy() {
			return nil
		}
		return errors.New("connection refused")
	}
	t.Cleanup(f.d.Stop)

	ctx := context.Background()
	p, err := st.CreateProject(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	f.svc, err = st.CreateService(ctx, store.Service{
		ProjectID: p.ID, Name: "web", Kind: "app", Repo: "o/r", Branch: "main", Port: 8080,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetVariables(ctx, f.svc.ID, map[string]string{"GREETING": "hi from ${{ SHED_SERVICE_NAME }}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDomain(ctx, f.svc.ID, "web.example.com", false); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) deploy(t *testing.T) store.Deployment {
	t.Helper()
	dep, err := f.d.Deploy(context.Background(), f.svc.ID, store.TriggerManual, Commit{SHA: "abc1234def"})
	if err != nil {
		t.Fatal(err)
	}
	return dep
}

// wait polls until the deployment satisfies cond.
func (f *fixture) wait(t *testing.T, id string, cond func(store.Deployment) bool) store.Deployment {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		dep, err := f.st.Deployment(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if cond(dep) {
			return dep
		}
		if time.Now().After(deadline) {
			t.Fatalf("deployment %s stuck in %s", id, dep.Status)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func terminal(d store.Deployment) bool { return d.Status.Terminal() }

// settle waits until no worker is running, so post-switch cleanup is done.
func (f *fixture) settle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.d.mu.Lock()
		n := len(f.d.workers)
		f.d.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("workers did not settle")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDeployHappyPath(t *testing.T) {
	f := newFixture(t)
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	if dep.Status != store.StatusActive || dep.Error != "" {
		t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
	}
	if want := "shed/" + f.svc.ID + ":" + dep.ID; dep.Image != want {
		t.Errorf("image = %q, want %q", dep.Image, want)
	}
	c, ok := f.docker.container(dep.ContainerID)
	if !ok || !c.Running {
		t.Fatalf("container %q not running", dep.ContainerID)
	}
	spec := f.docker.runs[0]
	for _, want := range []string{"GREETING=hi from web", "PORT=8080", "SHED_GIT_COMMIT_SHA=abc1234def", "SHED_PUBLIC_DOMAIN=web.example.com"} {
		if !strings.Contains(strings.Join(spec.Env, "\n"), want) {
			t.Errorf("env %v lacks %s", spec.Env, want)
		}
	}
	if len(spec.Aliases) != 0 {
		t.Errorf("started with aliases %v, want none until healthy", spec.Aliases)
	}
	if got := f.docker.resolve("web"); len(got) != 1 || got[0] != dep.ContainerID {
		t.Errorf("web resolves to %v, want [%s]", got, dep.ContainerID)
	}
	f.proxy.mu.Lock()
	routes := f.proxy.routes
	f.proxy.mu.Unlock()
	if len(routes) != 1 || routes[0].Host != "web.example.com" || routes[0].Upstream != "10.0.0.1:8080" {
		t.Errorf("routes = %+v", routes)
	}

	var lines []string
	var statuses []store.DeploymentStatus
	err := f.d.FollowLog(context.Background(), dep.ID,
		func(l string) { lines = append(lines, l) },
		func(s store.DeploymentStatus) { statuses = append(statuses, s) })
	if err != nil {
		t.Fatal(err)
	}
	log := strings.Join(lines, "\n")
	for _, want := range []string{"==> Building o/r@abc1234", "building shed/", "==> Starting container", "Healthy after"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if len(statuses) != 1 || statuses[0] != store.StatusActive {
		t.Errorf("statuses = %v, want [active]", statuses)
	}

	statusOf, err := f.d.ServiceStatuses(context.Background(), f.svc.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if statusOf[f.svc.ID] != StatusActive {
		t.Errorf("service status = %s, want active", statusOf[f.svc.ID])
	}
}

func TestDeployDetectsPort(t *testing.T) {
	tests := []struct {
		name     string
		port     int
		exposed  []int
		wantPort int
	}{
		{"lowest exposed", 0, []int{3000, 8080}, 3000},
		{"nothing exposed", 0, nil, 0},
		{"keeps configured", 9000, []int{80}, 9000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.docker.exposed = tt.exposed
			f.svc.Port = tt.port
			if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
				t.Fatal(err)
			}
			dep := f.wait(t, f.deploy(t).ID, terminal)
			f.settle(t)

			if dep.Status != store.StatusActive {
				t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
			}
			svc, err := f.st.Service(context.Background(), f.svc.ID)
			if err != nil {
				t.Fatal(err)
			}
			if svc.Port != tt.wantPort {
				t.Errorf("port = %d, want %d", svc.Port, tt.wantPort)
			}
			if got := strings.Contains(strings.Join(f.docker.runs[0].Env, "\n"), fmt.Sprintf("PORT=%d", tt.wantPort)); got != (tt.wantPort > 0) {
				t.Errorf("env %v: PORT injected = %v, want %v", f.docker.runs[0].Env, got, tt.wantPort > 0)
			}
		})
	}
}

func TestDeployReplacesPrevious(t *testing.T) {
	f := newFixture(t)
	first := f.wait(t, f.deploy(t).ID, terminal)
	second := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	if second.Status != store.StatusActive {
		t.Fatalf("second status = %s (%q), want active", second.Status, second.Error)
	}
	if first = f.wait(t, first.ID, terminal); first.Status != store.StatusRemoved {
		t.Errorf("first status = %s, want removed", first.Status)
	}
	if _, ok := f.docker.container(first.ContainerID); ok {
		t.Error("first container still exists")
	}
}

func TestFailedDeployKeepsPrevious(t *testing.T) {
	f := newFixture(t)
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	f.healthy = func() bool { return false }
	second := f.wait(t, f.deploy(t).ID, terminal)

	if second.Status != store.StatusFailed || !strings.Contains(second.Error, "health check timed out") {
		t.Fatalf("second = %s (%q), want failed health check", second.Status, second.Error)
	}
	if _, ok := f.docker.container(second.ContainerID); ok {
		t.Error("failed container was not removed")
	}
	if first = f.wait(t, first.ID, terminal); first.Status != store.StatusActive {
		t.Errorf("first status = %s, want active", first.Status)
	}
	if c, ok := f.docker.container(first.ContainerID); !ok || !c.Running {
		t.Error("previous container is not running")
	}
}

func TestFailedDeployRestartsStoppedPrevious(t *testing.T) {
	f := newFixture(t)
	if _, err := f.st.CreateVolume(context.Background(), f.svc.ID, "/data"); err != nil {
		t.Fatal(err)
	}
	first := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)

	f.healthy = func() bool { return false }
	if second := f.wait(t, f.deploy(t).ID, terminal); second.Status != store.StatusFailed {
		t.Fatalf("second status = %s, want failed", second.Status)
	}
	if c, ok := f.docker.container(first.ContainerID); !ok || !c.Running {
		t.Error("previous container was not restarted")
	}
}

func TestNewerDeploymentCancelsOlder(t *testing.T) {
	f := newFixture(t)
	f.builder.block.Store(true)
	first := f.deploy(t)
	<-f.builder.blocked

	f.builder.block.Store(false)
	second := f.deploy(t)

	if first = f.wait(t, first.ID, terminal); first.Status != store.StatusCanceled {
		t.Errorf("first status = %s, want canceled", first.Status)
	}
	if second = f.wait(t, second.ID, terminal); second.Status != store.StatusActive {
		t.Errorf("second status = %s (%q), want active", second.Status, second.Error)
	}
}

func TestCancel(t *testing.T) {
	f := newFixture(t)
	f.builder.block.Store(true)
	dep := f.deploy(t)
	<-f.builder.blocked

	got, err := f.d.Cancel(context.Background(), dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusCanceled || got.FinishedAt == nil {
		t.Errorf("after cancel: %s, finished %v; want canceled", got.Status, got.FinishedAt)
	}
	if _, err := f.d.Cancel(context.Background(), dep.ID); !errors.Is(err, ErrNotInProgress) {
		t.Errorf("second cancel: err = %v, want ErrNotInProgress", err)
	}
}

func TestReconcile(t *testing.T) {
	f := newFixture(t)
	active := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.docker.Remove(context.Background(), active.ContainerID)
	stale, err := f.st.CreateDeployment(context.Background(), store.Deployment{
		ServiceID: f.svc.ID, Status: store.StatusBuilding, Trigger: store.TriggerPush,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.d.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.wait(t, stale.ID, terminal); got.Status != store.StatusFailed {
		t.Errorf("stale status = %s, want failed", got.Status)
	}
	got := f.wait(t, active.ID, terminal)
	if c, ok := f.docker.container(got.ContainerID); !ok || !c.Running {
		t.Error("active container was not recreated")
	}
}
