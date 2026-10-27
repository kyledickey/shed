package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// fakeServices holds services and their active deployments in memory.
type fakeServices struct {
	mu       sync.Mutex
	services map[string]database // by service ID
	byID     map[string]store.Deployment
}

func newFakeServices(dbs ...database) *fakeServices {
	f := &fakeServices{services: map[string]database{}, byID: map[string]store.Deployment{}}
	for _, db := range dbs {
		f.put(db)
	}
	return f
}

// put adds or replaces a database and its active deployment.
func (f *fakeServices) put(db database) {
	f.mu.Lock()
	defer f.mu.Unlock()
	db.dep.Status = store.StatusActive
	f.services[db.svc.ID] = db
	f.byID[db.dep.ID] = db.dep
}

func (f *fakeServices) edit(id string, fn func(*database)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	db := f.services[id]
	fn(&db)
	f.services[id] = db
	f.byID[db.dep.ID] = db.dep
}

func (f *fakeServices) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.services, id)
}

func (f *fakeServices) AllServices(context.Context) ([]store.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Service
	for _, db := range f.services {
		out = append(out, db.svc)
	}
	return out, nil
}

func (f *fakeServices) Project(_ context.Context, id string) (store.Project, error) {
	return store.Project{ID: id, Name: "shop"}, nil
}

func (f *fakeServices) ActiveDeployment(_ context.Context, id string) (store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	db, ok := f.services[id]
	if !ok || db.dep.Status != store.StatusActive {
		return store.Deployment{}, store.ErrNotFound
	}
	return db.dep, nil
}

func (f *fakeServices) Deployment(_ context.Context, id string) (store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.byID[id]
	if !ok {
		return store.Deployment{}, store.ErrNotFound
	}
	return d, nil
}

// fakeExporters keeps exporter containers in memory.
type fakeExporters struct {
	mu     sync.Mutex
	ctrs   map[string]*docker.Container // by ID
	seq    int
	images map[string]bool
	calls  []string
	execs  []string // IDs of the containers commands ran in
	script string
	// execErr is returned by Exec.
	execErr error
}

func newFakeExporters() *fakeExporters {
	return &fakeExporters{ctrs: map[string]*docker.Container{}, images: map[string]bool{}}
}

func (d *fakeExporters) List(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []docker.Container
	for _, c := range d.ctrs {
		match := true
		for k, v := range labels {
			if got, ok := c.Labels[k]; !ok || v != "" && got != v {
				match = false
			}
		}
		if match {
			out = append(out, *c)
		}
	}
	slices.SortFunc(out, func(a, b docker.Container) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func (d *fakeExporters) ResolveImage(_ context.Context, ref string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.images[ref] {
		return "", errors.New("no such image")
	}
	return ref, nil
}

func (d *fakeExporters) PullImage(_ context.Context, ref string, _ io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, "pull "+ref)
	d.images[ref] = true
	return nil
}

func (d *fakeExporters) Run(_ context.Context, spec docker.RunSpec) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seq++
	id := fmt.Sprint("x", d.seq)
	d.ctrs[id] = &docker.Container{
		ID: id, Name: spec.Name, State: "running", Running: true, Labels: spec.Labels,
		IPs: map[string]string{spec.Network: fmt.Sprintf("10.0.0.%d", d.seq)},
	}
	d.calls = append(d.calls, "run "+spec.Name)
	return id, nil
}

func (d *fakeExporters) Start(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	c := d.ctrs[id]
	c.State, c.Running = "running", true
	d.calls = append(d.calls, "start "+c.Name)
	return nil
}

func (d *fakeExporters) Remove(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, "remove "+d.ctrs[id].Name)
	delete(d.ctrs, id)
	return nil
}

func (d *fakeExporters) Exec(_ context.Context, id string, cmd []string, _ io.Reader, _, _ io.Writer) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.execs = append(d.execs, id)
	d.script = strings.Join(cmd, " ")
	return d.execErr
}

// take returns the container calls made since the last take.
func (d *fakeExporters) take() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.calls
	d.calls = nil
	return out
}

type fakeRedeployer struct {
	mu    sync.Mutex
	svcs  *fakeServices
	calls []string
	// finish is the status a redeployment ends with.
	finish store.DeploymentStatus
}

func (r *fakeRedeployer) RedeployAs(_ context.Context, id string, trigger store.Trigger) (store.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, _ := r.svcs.Deployment(context.Background(), id)
	r.calls = append(r.calls, fmt.Sprintf("%s %s", old.ServiceID, trigger))
	next := old
	next.ID = old.ID + "+"
	r.svcs.mu.Lock()
	db := r.svcs.services[old.ServiceID]
	tmpl := testDatabase(db.svc.Kind, true)
	next.Runtime = &store.DeploymentRuntime{Cmd: tmpl.dep.Runtime.Cmd, Env: old.Runtime.Env}
	next.Status = r.finish
	db.dep = next
	if r.finish != store.StatusActive {
		db.dep = old
	}
	r.svcs.services[old.ServiceID] = db
	r.svcs.byID[next.ID] = next
	r.svcs.mu.Unlock()
	return next, nil
}

type harnessDB struct {
	d    *databases
	dock *fakeExporters
	svcs *fakeServices
	// published is the last set of targets handed to publish.
	published []scrapeTarget
}

func newDatabasesHarness(dbs ...database) *harnessDB {
	h := &harnessDB{dock: newFakeExporters(), svcs: newFakeServices(dbs...)}
	h.d = newDatabases(Config{Databases: h.svcs, Docker: nil, Log: slog.New(slog.DiscardHandler)}, func(t []scrapeTarget) error {
		h.published = t
		return nil
	})
	h.d.docker = h.dock
	h.d.settle = time.Millisecond
	return h
}

func (h *harnessDB) reconcile(t *testing.T) {
	t.Helper()
	if err := h.d.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestDatabasesReconcile(t *testing.T) {
	pg, redis := testDatabase("postgres", true), testDatabase("redis", true)
	h := newDatabasesHarness(pg, redis)
	h.reconcile(t)

	pgImage, redisImage := exportersByKind["postgres"].image, exportersByKind["redis"].image
	want := []string{"pull " + pgImage, "run shed-exp-svcpostgres", "pull " + redisImage, "run shed-exp-svcredis"}
	if got := h.dock.take(); !slices.Equal(got, want) {
		t.Fatalf("first reconcile did %v, want %v", got, want)
	}
	if len(h.published) != 2 || h.published[0].Addr != "10.0.0.1:9187" || h.published[1].Addr != "10.0.0.2:9121" {
		t.Errorf("targets = %+v", h.published)
	}
	if h.published[0].Labels["shed_service_id"] != "svcpostgres" || h.published[0].Labels["shed_source"] != "exporter" {
		t.Errorf("labels = %v", h.published[0].Labels)
	}

	h.reconcile(t)
	if got := h.dock.take(); len(got) != 0 {
		t.Errorf("reconciling an unchanged set did %v", got)
	}

	// A new password recreates the exporter.
	h.svcs.edit("svcpostgres", func(db *database) { db.dep.Runtime.Env["POSTGRES_PASSWORD"] = "rotated" })
	h.reconcile(t)
	if got, want := h.dock.take(), []string{"remove shed-exp-svcpostgres", "run shed-exp-svcpostgres"}; !slices.Equal(got, want) {
		t.Errorf("credential change did %v, want %v", got, want)
	}

	// A redeployment under the same credentials does not.
	h.svcs.edit("svcpostgres", func(db *database) { db.dep.ID = "dep-2"; db.dep.ContainerID = "ctr-2" })
	h.reconcile(t)
	if got := h.dock.take(); len(got) != 0 {
		t.Errorf("a new deployment did %v", got)
	}
	if h.published[0].Labels["shed_deployment_id"] != "dep-2" {
		t.Errorf("targets name deployment %q, want dep-2", h.published[0].Labels["shed_deployment_id"])
	}

	// A stopped exporter is started again.
	for _, c := range h.dock.ctrs {
		if c.Name == "shed-exp-svcredis" {
			c.State, c.Running = "exited", false
		}
	}
	h.reconcile(t)
	if got, want := h.dock.take(), []string{"start shed-exp-svcredis"}; !slices.Equal(got, want) {
		t.Errorf("exited exporter: %v, want %v", got, want)
	}

	// Stopping a database removes its exporter.
	h.svcs.edit("svcredis", func(db *database) { db.svc.Stopped = true })
	h.reconcile(t)
	if got, want := h.dock.take(), []string{"remove shed-exp-svcredis"}; !slices.Equal(got, want) {
		t.Errorf("stopped database: %v, want %v", got, want)
	}
	if len(h.published) != 1 {
		t.Errorf("targets = %+v after stopping a database", h.published)
	}

	// So does a deployment that stops being active.
	h.svcs.edit("svcpostgres", func(db *database) { db.dep.Status = store.StatusCrashed })
	h.reconcile(t)
	if got, want := h.dock.take(), []string{"remove shed-exp-svcpostgres"}; !slices.Equal(got, want) {
		t.Errorf("inactive deployment: %v, want %v", got, want)
	}
	if len(h.published) != 0 {
		t.Errorf("targets = %+v with no database left", h.published)
	}
}

func TestDatabasesRemoveDeleted(t *testing.T) {
	h := newDatabasesHarness(testDatabase("mongo", true), testDatabase("mysql", true))
	h.reconcile(t)
	h.dock.take()
	h.svcs.remove("svcmongo")
	h.reconcile(t)
	if got, want := h.dock.take(), []string{"remove shed-exp-svcmongo"}; !slices.Equal(got, want) {
		t.Errorf("deleted database: %v, want %v", got, want)
	}
}

func TestDatabasesRemoveAll(t *testing.T) {
	h := newDatabasesHarness(testDatabase("mongo", true), testDatabase("mysql", true))
	h.reconcile(t)
	h.dock.take()
	if err := h.d.removeAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.dock.ctrs) != 0 {
		t.Errorf("%d exporters left", len(h.dock.ctrs))
	}
}

func TestDatabasesSkipUnrecorded(t *testing.T) {
	legacy := testDatabase("redis", true)
	legacy.dep.Runtime = nil
	app := testDatabase("redis", true)
	app.svc.ID, app.svc.Kind = "svcapp", "app"
	h := newDatabasesHarness(legacy, app)
	h.reconcile(t)
	if got := h.dock.take(); len(got) != 0 {
		t.Errorf("exporters for a deployment without a recorded runtime and for an app: %v", got)
	}
}

func TestDatabasesCreateExtension(t *testing.T) {
	pg := testDatabase("postgres", true)
	old := testDatabase("postgres", false)
	old.svc.ID, old.dep.ID, old.dep.ContainerID = "svcold", "dep-old", "ctr-old"
	h := newDatabasesHarness(pg, old, testDatabase("redis", true))
	h.reconcile(t)
	if !slices.Equal(h.dock.execs, []string{"ctr-postgres"}) {
		t.Fatalf("commands ran in %v, want only the postgres started with statistics", h.dock.execs)
	}
	for _, want := range []string{"CREATE EXTENSION IF NOT EXISTS pg_stat_statements", "$POSTGRES_PASSWORD", "${POSTGRES_DB:"} {
		if !strings.Contains(h.dock.script, want) {
			t.Errorf("script lacks %q: %s", want, h.dock.script)
		}
	}

	h.reconcile(t)
	if len(h.dock.execs) != 1 {
		t.Errorf("extension created again for the same deployment: %v", h.dock.execs)
	}

	h.svcs.edit("svcpostgres", func(db *database) { db.dep.ID = "dep-2"; db.dep.ContainerID = "ctr-2" })
	h.reconcile(t)
	if !slices.Equal(h.dock.execs, []string{"ctr-postgres", "ctr-2"}) {
		t.Errorf("a new activation: commands ran in %v", h.dock.execs)
	}
}

func TestDatabasesCreateExtensionAfterRestore(t *testing.T) {
	h := newDatabasesHarness(testDatabase("postgres", true))
	h.reconcile(t)
	// A restore recreates the container of the same deployment on the
	// restored data.
	h.svcs.edit("svcpostgres", func(db *database) { db.dep.ContainerID = "ctr-restored" })
	h.reconcile(t)
	if !slices.Equal(h.dock.execs, []string{"ctr-postgres", "ctr-restored"}) {
		t.Errorf("commands ran in %v, want the restored container too", h.dock.execs)
	}
	if len(h.d.prepared) != 1 || !h.d.prepared["ctr-restored"] {
		t.Errorf("prepared = %v, want only the current container", h.d.prepared)
	}
}

func TestDatabasesCreateExtensionRetries(t *testing.T) {
	h := newDatabasesHarness(testDatabase("postgres", true))
	h.dock.execErr = errors.New("connection refused")
	h.reconcile(t)
	h.dock.execErr = nil
	h.reconcile(t)
	if len(h.dock.execs) != 2 {
		t.Errorf("commands ran %d times, want a retry after the failure", len(h.dock.execs))
	}
	h.reconcile(t)
	if len(h.dock.execs) != 2 {
		t.Errorf("commands ran %d times after success", len(h.dock.execs))
	}
}

func TestDatabasesUpgrade(t *testing.T) {
	has := testDatabase("postgres", true)
	without := testDatabase("mysql", false)
	redis := testDatabase("redis", false)
	redis.svc.Stopped = true
	custom := testDatabase("mongo", false)
	custom.svc.StartCommand = "mongod --custom"
	failing := testDatabase("redis", false)
	failing.svc.ID, failing.dep.ServiceID, failing.dep.ID = "svcredis2", "svcredis2", "dep-redis2"
	h := newDatabasesHarness(has, without, redis, custom, failing)
	r := &fakeRedeployer{svcs: h.svcs, finish: store.StatusActive}
	h.d.redeployer = r

	h.d.upgrade(context.Background())
	// Only a database that runs now, with no start command of its own,
	// and was started without statistics.
	if want := []string{"svcmysql telemetry", "svcredis2 telemetry"}; !slices.Equal(r.calls, want) {
		t.Fatalf("redeployed %v, want %v", r.calls, want)
	}

	r.calls = nil
	h.d.upgrade(context.Background())
	if len(r.calls) != 0 {
		t.Errorf("redeployed again: %v", r.calls)
	}
}

func TestDatabasesUpgradeOneAtATime(t *testing.T) {
	a, b := testDatabase("mysql", false), testDatabase("redis", false)
	h := newDatabasesHarness(a, b)
	// The first redeployment stays in progress until the test finishes it.
	r := &slowRedeployer{fakeRedeployer: fakeRedeployer{svcs: h.svcs, finish: store.StatusActive}}
	h.d.redeployer = r
	done := make(chan struct{})
	go func() { h.d.upgrade(context.Background()); close(done) }()

	deadline := time.Now().Add(5 * time.Second)
	for r.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if n := r.count(); n != 1 {
		t.Fatalf("%d redeployments started while the first runs, want 1", n)
	}
	r.release()
	<-done
	if n := r.count(); n != 2 {
		t.Errorf("%d redeployments, want 2", n)
	}
}

// slowRedeployer leaves its first deployment queued until released.
type slowRedeployer struct {
	fakeRedeployer
	mu2    sync.Mutex
	n      int
	queued store.Deployment
}

func (r *slowRedeployer) count() int {
	r.mu2.Lock()
	defer r.mu2.Unlock()
	return r.n
}

func (r *slowRedeployer) RedeployAs(ctx context.Context, id string, trigger store.Trigger) (store.Deployment, error) {
	r.mu2.Lock()
	r.n++
	first := r.n == 1
	r.mu2.Unlock()
	dep, err := r.fakeRedeployer.RedeployAs(ctx, id, trigger)
	if first {
		r.svcs.mu.Lock()
		dep.Status = store.StatusDeploying
		r.svcs.byID[dep.ID] = dep
		r.queued = dep
		r.svcs.mu.Unlock()
	}
	return dep, err
}

func (r *slowRedeployer) release() {
	r.svcs.mu.Lock()
	defer r.svcs.mu.Unlock()
	d := r.queued
	d.Status = store.StatusActive
	r.svcs.byID[d.ID] = d
}

func TestDatabasesUpgradeFailure(t *testing.T) {
	h := newDatabasesHarness(testDatabase("mysql", false), testDatabase("redis", false))
	r := &fakeRedeployer{svcs: h.svcs, finish: store.StatusFailed}
	h.d.redeployer = r
	h.d.upgrade(context.Background())
	if len(r.calls) != 2 {
		t.Errorf("a failed redeployment stopped the others: %v", r.calls)
	}
}

func TestStartedWithStatements(t *testing.T) {
	for _, kind := range []string{"postgres", "mysql", "mongo", "redis"} {
		if !testDatabase(kind, true).statements() {
			t.Errorf("%s started with the template's command reports no statistics", kind)
		}
		if testDatabase(kind, false).statements() {
			t.Errorf("%s started with the image default reports statistics", kind)
		}
	}
	custom := testDatabase("redis", true)
	custom.svc.StartCommand = "redis-server"
	if custom.statements() {
		t.Error("a database with its own start command reports statistics")
	}
}

func TestManagerRunsExporters(t *testing.T) {
	svcs := newFakeServices(testDatabase("redis", true))
	h := newHarness(t, func(c *Config) { c.Databases = svcs })
	scrape := func() string {
		b, err := os.ReadFile(filepath.Join(h.dir, "scrape", "scrape.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	exporterNames := func() []string {
		list, _ := h.docker.List(context.Background(), map[string]string{LabelExporter: ""})
		var names []string
		for _, c := range list {
			names = append(names, c.Name)
		}
		return names
	}
	eventually := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	h.set(t, enabled())
	eventually("the exporter's scrape target", func() bool { return strings.Contains(scrape(), ":9121") })
	if got := exporterNames(); !slices.Equal(got, []string{"shed-exp-svcredis"}) {
		t.Errorf("exporters = %v", got)
	}
	var removed string
	list, _ := h.docker.List(context.Background(), map[string]string{LabelExporter: "svcredis"})
	for _, ip := range list[0].IPs {
		removed = ip + ":9121"
	}
	if s := scrape(); !strings.Contains(s, "shed_service_id: svcredis") || !strings.Contains(s, "shed-telemetry") {
		t.Errorf("scrape.yaml:\n%s", s)
	}

	// Reconciling again does not drop the exporters from scrape.yaml.
	h.set(t, enabled())
	if !strings.Contains(scrape(), ":9121") {
		t.Errorf("scrape.yaml lost the exporter on reconcile:\n%s", scrape())
	}

	off := enabled()
	off.Enabled = false
	h.set(t, off)
	if got := exporterNames(); len(got) != 0 {
		t.Errorf("exporters after disable: %v", got)
	}

	// The worker publishes the new exporter right away, so look for the
	// address of the removed one.
	h.set(t, enabled())
	if strings.Contains(scrape(), removed) {
		t.Errorf("scrape.yaml names an exporter that was removed, %s:\n%s", removed, scrape())
	}
	eventually("the exporter after enabling again", func() bool { return len(exporterNames()) == 1 })
}
