package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// clock is a settable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

type testEnv struct {
	t        *testing.T
	ctx      context.Context
	st       *store.Store
	rec      *recorder
	docker   *fakeDocker
	services *fakeServices
	remote   *fakeRemote
	clock    *clock
	dir      string
	m        *Manager
	project  string

	mu      sync.Mutex
	configs []S3Config
	buckets map[string]*fakeRemote // remotes by bucket; others use remote
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p, err := st.CreateProject(ctx, "p")
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	e := &testEnv{
		t:       t,
		ctx:     ctx,
		st:      st,
		rec:     rec,
		docker:  newFakeDocker(rec),
		remote:  &fakeRemote{objects: make(map[string][]byte)},
		clock:   &clock{t: time.Date(2026, 10, 4, 2, 59, 30, 0, time.UTC)},
		dir:     filepath.Join(t.TempDir(), "backups"),
		project: p.ID,
		buckets: make(map[string]*fakeRemote),
	}
	e.services = &fakeServices{rec: rec, st: st, docker: e.docker}
	e.m = New(Config{
		Store:    st,
		Docker:   e.docker,
		Services: e.services,
		NewRemote: func(c S3Config) (Remote, error) {
			e.mu.Lock()
			e.configs = append(e.configs, c)
			e.mu.Unlock()
			if c.Bucket == "" || c.SecretAccessKey == "" {
				return nil, errors.New("s3: bucket and secret are required")
			}
			if r, ok := e.buckets[c.Bucket]; ok {
				return r, nil
			}
			return e.remote, nil
		},
		Dir: e.dir,
		Now: e.clock.Now,
	})
	return e
}

// service creates a service of kind with volumes at mounts and an active
// deployment whose container is running or not. Without mounts it has no
// volume; with deployed false it has no deployment.
func (e *testEnv) service(kind string, running bool, mounts ...string) store.Service {
	e.t.Helper()
	sv, err := e.st.CreateService(e.ctx, store.Service{ProjectID: e.project, Name: kind + store.NewID()[:4], Kind: kind})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, mp := range mounts {
		if _, err := e.st.CreateVolume(e.ctx, sv.ID, mp); err != nil {
			e.t.Fatal(err)
		}
	}
	cid := "ctr-" + sv.ID
	if _, err := e.st.CreateDeployment(e.ctx, store.Deployment{
		ServiceID: sv.ID, Status: store.StatusActive, Trigger: store.TriggerCreate, Image: "img-" + kind, ContainerID: cid,
	}); err != nil {
		e.t.Fatal(err)
	}
	e.docker.mu.Lock()
	e.docker.containers[cid] = docker.Container{ID: cid, Running: running}
	e.docker.mu.Unlock()
	return sv
}

func (e *testEnv) setRunning(sv store.Service, running bool) {
	e.docker.mu.Lock()
	defer e.docker.mu.Unlock()
	cid := "ctr-" + sv.ID
	e.docker.containers[cid] = docker.Container{ID: cid, Running: running}
}

// drain runs the queued jobs in order.
func (e *testEnv) drain() {
	e.t.Helper()
	for {
		e.m.mu.Lock()
		n := len(e.m.queue)
		e.m.mu.Unlock()
		if n == 0 {
			return
		}
		e.m.execute(e.ctx, e.m.take(e.ctx))
	}
}

func (e *testEnv) backup(id string) store.Backup {
	e.t.Helper()
	b, err := e.st.Backup(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return b
}

// backUp runs a manual backup and returns it, requiring success.
func (e *testEnv) backUp(serviceID string) store.Backup {
	e.t.Helper()
	b, err := e.m.BackUp(e.ctx, serviceID)
	if err != nil {
		e.t.Fatal(err)
	}
	e.drain()
	b = e.backup(b.ID)
	if b.Status != store.BackupSucceeded {
		e.t.Fatalf("backup = %+v, want succeeded", b)
	}
	return b
}

// content returns the decoded local archive of b.
func (e *testEnv) content(b store.Backup) string {
	e.t.Helper()
	rc, err := e.m.Open(e.ctx, b.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rc.Close()
	d, err := decompress(rc)
	if err != nil {
		e.t.Fatal(err)
	}
	defer d.Close()
	data, err := io.ReadAll(d)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(data)
}

func (e *testEnv) setS3(prefix string) {
	e.t.Helper()
	_, err := e.m.SetSettings(e.ctx, SettingsInput{S3: &S3Input{
		Endpoint: "https://s3.example.com", Bucket: "b", Prefix: prefix, AccessKeyID: "AK", SecretAccessKey: "SK",
	}})
	if err != nil {
		e.t.Fatal(err)
	}
}

// bucket returns a remote of its own for bucket, for destinations other than
// the default one.
func (e *testEnv) bucket(name string) *fakeRemote {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.buckets[name]
	if !ok {
		r = &fakeRemote{objects: make(map[string][]byte)}
		e.buckets[name] = r
	}
	return r
}

func (e *testEnv) setPolicy(serviceID string, edit func(*PolicyInput)) {
	e.t.Helper()
	p := defaultPolicy
	edit(&p)
	if _, err := e.m.SetPolicy(e.ctx, serviceID, p); err != nil {
		e.t.Fatal(err)
	}
}

// partials returns the partial files left under the backup directory.
func (e *testEnv) partials() []string {
	var out []string
	filepath.WalkDir(e.dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, partialSuffix) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func TestChooseMethod(t *testing.T) {
	tests := []struct {
		kind    string
		running bool
		want    store.BackupMethod
	}{
		{"postgres", true, store.MethodDump},
		{"mysql", true, store.MethodDump},
		{"mongo", true, store.MethodDump},
		{"redis", true, store.MethodDump},
		{"postgres", false, store.MethodVolume},
		{"redis", false, store.MethodVolume},
		{"app", true, store.MethodVolume},
		{"app", false, store.MethodVolume},
	}
	for _, tt := range tests {
		if got := chooseMethod(tt.kind, tt.running); got != tt.want {
			t.Errorf("chooseMethod(%s, %v) = %s, want %s", tt.kind, tt.running, got, tt.want)
		}
	}
}

func TestBackUpDump(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.dump = "-- SQL dump\n"

	b, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != store.BackupQueued || b.Method != store.MethodDump || b.Trigger != store.BackupManual {
		t.Errorf("queued backup = %+v", b)
	}
	e.drain()
	b = e.backup(b.ID)
	if b.Status != store.BackupSucceeded || !b.Local || b.Encrypted || b.File != b.ID+".sql.zst" || b.Size == 0 {
		t.Errorf("backup = %+v", b)
	}
	if _, err := os.Stat(filepath.Join(e.dir, sv.ID, b.File)); err != nil {
		t.Error(err)
	}
	if got := e.content(b); got != e.docker.dump {
		t.Errorf("archive = %q, want %q", got, e.docker.dump)
	}
	if got, want := e.rec.list(), []string{"exec ctr-" + sv.ID + " dump"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %q, want %q", got, want)
	}
}

func TestBackUpErrors(t *testing.T) {
	e := newEnv(t)
	noVolumes := e.service("app", true)
	if _, err := e.m.BackUp(e.ctx, noVolumes.ID); !errors.Is(err, ErrNoVolumes) {
		t.Errorf("BackUp without volumes = %v, want ErrNoVolumes", err)
	}

	undeployed, err := e.st.CreateService(e.ctx, store.Service{ProjectID: e.project, Name: "new", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	e.st.CreateVolume(e.ctx, undeployed.ID, "/var/lib/postgresql")
	if _, err := e.m.BackUp(e.ctx, undeployed.ID); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not been deployed") {
		t.Errorf("BackUp of an undeployed service = %v, want ErrInvalid", err)
	}

	if _, err := e.m.BackUp(e.ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("BackUp of an unknown service = %v, want ErrNotFound", err)
	}

	sv := e.service("redis", true, "/data")
	if _, err := e.m.BackUp(e.ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.BackUp(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("second BackUp = %v, want ErrBusy", err)
	}
	if _, err := e.m.BackUp(e.ctx, ""); err != nil {
		t.Errorf("BackUp of shed.db while a service backup is queued = %v", err)
	}
}

func TestBackUpStoppedDatabase(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", false, "/var/lib/postgresql")
	vols, _ := e.st.Volumes(e.ctx, sv.ID)
	e.docker.volumes["/var/lib/postgresql"] = volumeTar("/var/lib/postgresql", map[string]string{"18/docker/PG_VERSION": "18"})

	b := e.backUp(sv.ID)
	if b.Method != store.MethodVolume || b.File != b.ID+".tar.zst" {
		t.Errorf("backup = %+v, want a volume archive", b)
	}
	want := []string{
		"hold " + sv.ID,
		"create shed-backup-" + b.ID + " img-postgres shed-vol-" + vols[0].ID + ":/var/lib/postgresql:ro=true",
		"copy from helper1 /var/lib/postgresql",
		"remove helper1",
		"release " + sv.ID,
	}
	if got := e.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n got %q\nwant %q", got, want)
	}
	var names []string
	for _, en := range readTar(t, strings.NewReader(e.content(b))) {
		names = append(names, en.name)
	}
	if want := []string{"var/lib/postgresql/", "var/lib/postgresql/18/docker/PG_VERSION"}; !reflect.DeepEqual(names, want) {
		t.Errorf("archive entries = %q, want %q", names, want)
	}
}

func TestBackUpAppVolumeWithoutHold(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv/a", "/srv/b")
	e.docker.volumes["/srv/a"] = volumeTar("/srv/a", map[string]string{"x": "1"})
	e.docker.volumes["/srv/b"] = volumeTar("/srv/b", map[string]string{"y": "2"})
	b := e.backUp(sv.ID)
	for _, ev := range e.rec.list() {
		if strings.HasPrefix(ev, "hold") {
			t.Errorf("app volume backup held the service: %q", e.rec.list())
		}
	}
	var names []string
	for _, en := range readTar(t, strings.NewReader(e.content(b))) {
		names = append(names, en.name)
	}
	if want := []string{"srv/a/", "srv/a/x", "srv/b/", "srv/b/y"}; !reflect.DeepEqual(names, want) {
		t.Errorf("archive entries = %q, want %q", names, want)
	}
}

func TestBackUpMethodChangesWithState(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.volumes["/var/lib/postgresql"] = volumeTar("/var/lib/postgresql", nil)
	b, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil || b.Method != store.MethodDump {
		t.Fatalf("BackUp = %+v, %v; want a queued dump", b, err)
	}
	e.setRunning(sv, false) // Stopped while queued.
	e.drain()
	got := e.backup(b.ID)
	if got.Status != store.BackupSucceeded || got.Method != store.MethodVolume || !got.CreatedAt.Equal(b.CreatedAt) {
		t.Errorf("backup = %+v, want a successful volume backup with the original creation time", got)
	}
}

func TestBackUpFailure(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.execErr = &docker.ExitError{Code: 1}
	b, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.drain()
	b = e.backup(b.ID)
	if b.Status != store.BackupFailed || !strings.Contains(b.Error, "connection refused") || b.FinishedAt == nil {
		t.Errorf("backup = %+v, want failed with the command's stderr", b)
	}
	if p := e.partials(); len(p) > 0 {
		t.Errorf("partial files left: %v", p)
	}
	entries, _ := os.ReadDir(filepath.Join(e.dir, sv.ID))
	if len(entries) != 0 {
		t.Errorf("files left after a failed backup: %v", entries)
	}
}

func TestSystemBackup(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	b := e.backUp("")
	if b.Method != store.MethodSQLite || !b.Encrypted || b.File != b.ID+".db.zst.age" || b.ServiceID != "" {
		t.Errorf("backup = %+v", b)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "system", b.File)); err != nil {
		t.Error(err)
	}
	if got := e.content(b); !strings.HasPrefix(got, "SQLite format 3\x00") {
		t.Errorf("archive does not hold a SQLite database: %q", got[:min(len(got), 16)])
	}
	if p := e.partials(); len(p) > 0 {
		t.Errorf("partial files left: %v", p)
	}
}

func TestUpload(t *testing.T) {
	e := newEnv(t)
	e.setS3("/backups/")
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.dump = "dump"

	b := e.backUp(sv.ID)
	if b.RemoteKey != "backups/services/"+sv.ID+"/"+b.File || !b.Local || b.RemoteError != "" {
		t.Errorf("backup = %+v, want uploaded and kept locally", b)
	}

	e.setPolicy(sv.ID, func(p *PolicyInput) { p.KeepLocal = 0 })
	b = e.backUp(sv.ID)
	if b.Local || b.RemoteKey == "" {
		t.Errorf("backup = %+v, want only in S3 with keep_local 0", b)
	}
	if _, err := os.Stat(filepath.Join(e.dir, sv.ID, b.File)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("local file of an uploaded keep_local 0 backup: %v", err)
	}
	if got := e.content(b); got != "dump" {
		t.Errorf("archive from S3 = %q", got)
	}

	e.remote.putErr = errors.New("access denied")
	b = e.backUp(sv.ID)
	if !b.Local || b.RemoteKey != "" || !strings.Contains(b.RemoteError, "access denied") || b.Status != store.BackupSucceeded {
		t.Errorf("backup = %+v, want kept locally with the upload error", b)
	}
	e.remote.putErr = nil

	sys := e.backUp("")
	if sys.RemoteKey != "backups/system/"+sys.File {
		t.Errorf("system backup key = %q", sys.RemoteKey)
	}

	if _, err := e.m.SetSettings(e.ctx, SettingsInput{}); err != nil {
		t.Fatal(err)
	}
	b = e.backUp(sv.ID)
	if !b.Local || b.RemoteKey != "" || b.RemoteError != "" {
		t.Errorf("backup without S3 = %+v, want kept locally", b)
	}
}

func TestUploadingStatus(t *testing.T) {
	e := newEnv(t)
	e.setS3("")
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.remote.putting, e.remote.proceed = make(chan struct{}), make(chan struct{})
	b, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.drain()
	}()
	<-e.remote.putting
	got := e.backup(b.ID)
	if got.Status != store.BackupUploading || !got.Local || got.Size == 0 || got.FinishedAt != nil {
		t.Errorf("backup during upload = %+v, want uploading", got)
	}
	if err := e.m.Delete(e.ctx, b.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("Delete during upload = %v, want ErrBusy", err)
	}
	if _, err := e.m.BackUp(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("BackUp during upload = %v, want ErrBusy", err)
	}
	close(e.remote.proceed)
	<-done
	got = e.backup(b.ID)
	if got.Status != store.BackupSucceeded || got.RemoteKey == "" || got.FinishedAt == nil {
		t.Errorf("backup after upload = %+v, want succeeded and uploaded", got)
	}

	// Without a destination, a backup never shows as uploading.
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{}); err != nil {
		t.Fatal(err)
	}
	if b := e.backUp(sv.ID); b.RemoteKey != "" || b.RemoteError != "" {
		t.Errorf("backup without S3 = %+v", b)
	}
}

func TestRemoteBackupsKeepTheirDestination(t *testing.T) {
	e := newEnv(t)
	e.setS3("old")
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.setPolicy(sv.ID, func(p *PolicyInput) { p.KeepLocal = 0 })
	e.docker.dump = "first"
	first := e.backUp(sv.ID)
	if first.Local || first.DestinationID == "" {
		t.Fatalf("backup = %+v, want only in S3", first)
	}

	// Move to another bucket and prefix.
	moved := e.bucket("other")
	in := S3Input{Endpoint: "https://s3.example.com", Bucket: "other", Prefix: "new", AccessKeyID: "AK2", SecretAccessKey: "SK2"}
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{S3: &in}); err != nil {
		t.Fatal(err)
	}
	e.docker.dump = "second"
	second := e.backUp(sv.ID)
	if second.DestinationID == first.DestinationID || second.RemoteKey != "new/services/"+sv.ID+"/"+second.File {
		t.Fatalf("backup after moving = %+v, want the new destination", second)
	}
	if len(moved.keys()) != 1 || len(e.remote.keys()) != 1 {
		t.Fatalf("objects: old %v, new %v", e.remote.keys(), moved.keys())
	}

	// The first backup is still read from where it was uploaded, also
	// once S3 is turned off.
	if got := e.content(first); got != "first" {
		t.Errorf("first backup after moving = %q", got)
	}
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{}); err != nil {
		t.Fatal(err)
	}
	if got := e.content(first); got != "first" {
		t.Errorf("first backup without S3 = %q", got)
	}
	if _, err := e.m.Restore(e.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := mustLatestRestore(t, e, sv.ID); r.Status != store.RestoreSucceeded || e.docker.restored != "first" {
		t.Fatalf("restore = %+v, restored %q", r, e.docker.restored)
	}

	// Saving the old location again reuses its destination, with new
	// credentials.
	e.setS3("old")
	d, err := e.m.destination(e.ctx)
	if err != nil || d.ID != first.DestinationID {
		t.Fatalf("destination = %+v, %v; want %s again", d, err, first.DestinationID)
	}

	// Deleting removes each object from its own destination.
	if err := e.m.Delete(e.ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.m.Delete(e.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if len(moved.keys()) != 0 || len(e.remote.keys()) != 0 {
		t.Errorf("objects after deleting: old %v, new %v", e.remote.keys(), moved.keys())
	}
}

func TestPruneUsesEachBackupsDestination(t *testing.T) {
	e := newEnv(t)
	e.setS3("")
	oldDest, err := e.m.destination(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	moved := e.bucket("other")
	in := S3Input{Endpoint: "https://s3.example.com", Bucket: "other", AccessKeyID: "AK", SecretAccessKey: "SK"}
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{S3: &in}); err != nil {
		t.Fatal(err)
	}
	newDest, err := e.m.destination(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.remote.objects["k-old"] = []byte("x")
	moved.objects["k-new"] = []byte("y")
	for i, spec := range []struct{ key, dest string }{{"k-new", newDest.ID}, {"k-old", oldDest.ID}} {
		if _, err := e.st.CreateBackup(e.ctx, store.Backup{
			ServiceID: sv.ID, Trigger: store.BackupSchedule, Method: store.MethodDump, Status: store.BackupSucceeded,
			File: spec.key, RemoteKey: spec.key, DestinationID: spec.dest,
			CreatedAt: time.Date(2026, 10, 4-i, 3, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.m.prune(e.ctx, sv.ID, PolicyInput{Upload: true, KeepRemote: 1}); err != nil {
		t.Fatal(err)
	}
	if len(e.remote.keys()) != 0 || len(moved.keys()) != 1 {
		t.Errorf("objects after pruning: old %v, new %v; want the old one deleted", e.remote.keys(), moved.keys())
	}
}

func TestRetention(t *testing.T) {
	sched := func(id string, local bool, remote string) store.Backup {
		return store.Backup{ID: id, Trigger: store.BackupSchedule, Status: store.BackupSucceeded, Local: local, RemoteKey: remote}
	}
	tests := []struct {
		name                  string
		bs                    []store.Backup // newest first
		keepLocal, keepRemote int
		upload                bool
		dropLocal, dropRemote []string
	}{
		{
			name:      "local only",
			bs:        []store.Backup{sched("4", true, ""), sched("3", true, ""), sched("2", true, ""), sched("1", true, "")},
			keepLocal: 2, dropLocal: []string{"2", "1"},
		},
		{
			name: "local and remote",
			bs: []store.Backup{
				sched("4", true, "k4"), sched("3", true, "k3"), sched("2", false, "k2"), sched("1", false, "k1"),
			},
			keepLocal: 1, keepRemote: 3, upload: true,
			dropLocal: []string{"3"}, dropRemote: []string{"1"},
		},
		{
			name:      "remote only kept while not uploading",
			bs:        []store.Backup{sched("2", true, "k2"), sched("1", true, "k1")},
			keepLocal: 5, keepRemote: 0, upload: false,
		},
		{
			name: "keep local 0 keeps backups not in S3",
			bs: []store.Backup{
				sched("3", false, "k3"), sched("2", true, ""), sched("1", true, "k1"),
			},
			keepLocal: 0, keepRemote: 5, upload: true,
			dropLocal: []string{"1"},
		},
		{
			name: "manual, pre-restore, and failed backups are exempt",
			bs: []store.Backup{
				{ID: "m", Trigger: store.BackupManual, Status: store.BackupSucceeded, Local: true, RemoteKey: "km"},
				{ID: "p", Trigger: store.BackupPreRestore, Status: store.BackupSucceeded, Local: true},
				{ID: "f", Trigger: store.BackupSchedule, Status: store.BackupFailed},
				sched("2", true, "k2"), sched("1", true, "k1"),
			},
			keepLocal: 1, keepRemote: 1, upload: true,
			dropLocal: []string{"1"}, dropRemote: []string{"1"},
		},
	}
	for _, tt := range tests {
		p := PolicyInput{KeepLocal: tt.keepLocal, KeepRemote: tt.keepRemote, Upload: tt.upload}
		dropLocal, dropRemote := retention(tt.bs, p)
		if got := keys(dropLocal); !slices.Equal(got, sorted(tt.dropLocal)) {
			t.Errorf("%s: drop local = %v, want %v", tt.name, got, tt.dropLocal)
		}
		if got := keys(dropRemote); !slices.Equal(got, sorted(tt.dropRemote)) {
			t.Errorf("%s: drop remote = %v, want %v", tt.name, got, tt.dropRemote)
		}
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func TestPruneAfterScheduledBackups(t *testing.T) {
	e := newEnv(t)
	e.setS3("")
	sv := e.service("redis", true, "/data")
	e.setPolicy(sv.ID, func(p *PolicyInput) { p.KeepLocal, p.KeepRemote = 1, 2 })
	manual := e.backUp(sv.ID)

	var scheduled []store.Backup
	for i := range 4 {
		e.clock.Set(time.Date(2026, 10, 4+i, 3, 0, 30, 0, time.UTC))
		b, err := e.m.enqueueBackup(e.ctx, sv.ID, store.BackupSchedule)
		if err != nil {
			t.Fatal(err)
		}
		e.drain()
		scheduled = append(scheduled, b)
	}
	bs, err := e.st.Backups(e.ctx, sv.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	type state struct {
		local, remote bool
	}
	got := make(map[string]state)
	for _, b := range bs {
		got[b.ID] = state{b.Local, b.RemoteKey != ""}
	}
	want := map[string]state{
		manual.ID:       {true, true},
		scheduled[3].ID: {true, true},
		scheduled[2].ID: {false, true},
		// scheduled[0] and [1] lost both copies and were deleted.
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("backups after pruning = %v, want %v", got, want)
	}
	if n := len(e.remote.keys()); n != 3 {
		t.Errorf("%d objects in S3, want 3", n)
	}
	files, _ := os.ReadDir(filepath.Join(e.dir, sv.ID))
	if len(files) != 2 {
		t.Errorf("%d local files, want 2", len(files))
	}
}

func TestDeleteFailedBackupsAfter30Days(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	old, err := e.st.CreateBackup(e.ctx, store.Backup{
		ServiceID: sv.ID, Trigger: store.BackupSchedule, Method: store.MethodDump, Status: store.BackupFailed,
		CreatedAt: e.clock.Now().Add(-31 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	e.backUp(sv.ID)
	if _, err := e.st.Backup(e.ctx, old.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("old failed backup still exists: %v", err)
	}
}

func TestRestoreVolume(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv/data", "/srv/cache")
	vols, _ := e.st.Volumes(e.ctx, sv.ID)
	volName := map[string]string{}
	for _, v := range vols {
		volName[v.MountPath] = volumeName(v)
	}
	e.docker.volumes["/srv/data"] = volumeTar("/srv/data", map[string]string{"f": "original"})
	e.docker.volumes["/srv/cache"] = volumeTar("/srv/cache", map[string]string{"c": "cache"})
	b := e.backUp(sv.ID)

	// Only /srv/data is in the next archive's source, so restoring an
	// archive that lacks a volume leaves that volume alone.
	e.docker.volumes["/srv/data"] = volumeTar("/srv/data", map[string]string{"f": "changed"})
	if _, err := e.st.CreateVolume(e.ctx, sv.ID, "/srv/new"); err != nil {
		t.Fatal(err)
	}
	e.docker.volumes["/srv/new"] = volumeTar("/srv/new", nil)
	e.rec = &recorder{}
	e.docker.rec, e.services.rec = e.rec, e.rec

	r, err := e.m.Restore(e.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != store.RestoreRunning || r.ServiceID != sv.ID || r.BackupID != b.ID {
		t.Errorf("restore = %+v", r)
	}
	e.drain()
	got, err := e.st.LatestRestore(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.RestoreSucceeded || got.FinishedAt == nil {
		t.Fatalf("restore = %+v, want succeeded", got)
	}

	events := e.rec.list()
	hold := slices.Index(events, "hold "+sv.ID)
	if hold < 0 {
		t.Fatalf("no hold in %q", events)
	}
	pre := events[:hold]
	if !slices.ContainsFunc(pre, func(ev string) bool { return strings.HasPrefix(ev, "copy from") }) {
		t.Errorf("no pre-restore backup before the hold: %q", events)
	}
	data, cache := volName["/srv/data"], volName["/srv/cache"]
	want := []string{
		"hold " + sv.ID,
		"stop and remove " + sv.ID,
		// The current data is copied aside and checked first.
		"remove volume " + data + "-pre-restore",
		"ensure volume " + data + "-pre-restore",
		"remove volume " + cache + "-pre-restore",
		"ensure volume " + cache + "-pre-restore",
		"create shed-restore-" + r.ID + "-src img-app " + data + ":/srv/data:ro=true," + cache + ":/srv/cache:ro=true",
		"create shed-restore-" + r.ID + "-dst img-app " + data + "-pre-restore:/srv/data:ro=false," + cache + "-pre-restore:/srv/cache:ro=false",
		"copy from helper3 /srv/data",
		"copy from helper3 /srv/cache",
		"copy to helper4 /",
		"copy from helper4 /srv/data",
		"copy from helper4 /srv/cache",
		"remove helper4",
		"remove helper3",
		// Only then are the volumes replaced, and checked.
		"remove volume " + data,
		"ensure volume " + data,
		"remove volume " + cache,
		"ensure volume " + cache,
		"create shed-restore-" + r.ID + " img-app " + data + ":/srv/data:ro=false," + cache + ":/srv/cache:ro=false",
		"copy to helper5 /",
		"copy from helper5 /srv/data",
		"copy from helper5 /srv/cache",
		"remove helper5",
		"remove volume " + data + "-pre-restore",
		"remove volume " + cache + "-pre-restore",
		"release " + sv.ID,
	}
	if got := events[hold:]; !reflect.DeepEqual(got, want) {
		t.Errorf("restore events:\n got %q\nwant %q", got, want)
	}
	var names []string
	for _, en := range readTar(t, bytes.NewReader(e.docker.extracted)) {
		names = append(names, en.name+"="+en.body)
	}
	if want := []string{"srv/data/=", "srv/data/f=original", "srv/cache/=", "srv/cache/c=cache"}; !reflect.DeepEqual(names, want) {
		t.Errorf("extracted %q, want %q", names, want)
	}
	if got := e.docker.volumeFiles(data); !reflect.DeepEqual(got, map[string]string{"f": "original"}) {
		t.Errorf("restored volume holds %q", got)
	}
	if fs, _ := e.st.RestoreFences(e.ctx); len(fs) != 0 {
		t.Errorf("fences left: %+v", fs)
	}
	if sv, _ := e.st.Service(e.ctx, sv.ID); sv.Stopped {
		t.Error("service left stopped after a successful restore")
	}

	bs, _ := e.st.Backups(e.ctx, sv.ID, 0)
	if len(bs) != 2 || bs[0].Trigger != store.BackupPreRestore || bs[0].Status != store.BackupSucceeded {
		t.Errorf("backups = %+v, want a pre-restore backup", bs)
	}
}

func TestRestoreDump(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.dump = "-- original\n"
	b := e.backUp(sv.ID)
	e.docker.dump = "-- current\n"
	e.rec = &recorder{}
	e.docker.rec, e.services.rec = e.rec, e.rec

	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r, _ := e.st.LatestRestore(e.ctx, sv.ID); r.Status != store.RestoreSucceeded {
		t.Fatalf("restore = %+v", r)
	}
	cid := "ctr-" + sv.ID
	want := []string{"exec " + cid + " dump", "hold " + sv.ID, "exec " + cid + " restore", "release " + sv.ID}
	if got := e.rec.list(); !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n got %q\nwant %q", got, want)
	}
	if e.docker.restored != "-- original\n" {
		t.Errorf("restored %q", e.docker.restored)
	}
}

func TestRestoreDumpFailureStopsDatabase(t *testing.T) {
	// cancel cancels the running job, as shutdown does, and returns what an
	// exec whose stream was aborted returns.
	cancel := func(e *testEnv) func(context.Context) error {
		return func(ctx context.Context) error {
			e.m.mu.Lock()
			e.m.running.cancel(nil)
			e.m.mu.Unlock()
			<-ctx.Done()
			return ctx.Err()
		}
	}
	tests := []struct {
		name       string
		hook       func(e *testEnv) func(context.Context) error
		stopErr    error
		wantError  string
		wantFenced bool
	}{
		{name: "load fails", hook: func(*testEnv) func(context.Context) error {
			return func(context.Context) error { return &docker.ExitError{Code: 3} }
		}, wantError: "may hold partial data"},
		{name: "canceled", hook: cancel},
		{name: "canceled, stopping fails", hook: cancel, stopErr: errors.New("daemon gone"), wantFenced: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			sv := e.service("postgres", true, "/var/lib/postgresql")
			b := e.backUp(sv.ID)
			e.rec = &recorder{}
			e.docker.rec, e.services.rec = e.rec, e.rec
			e.docker.restoreHook = tt.hook(e)
			e.services.stopErr = tt.stopErr
			if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
				t.Fatal(err)
			}
			e.drain()
			r := mustLatestRestore(t, e, sv.ID)
			if r.Status != store.RestoreFailed || !strings.Contains(r.Error, tt.wantError) {
				t.Errorf("restore = %+v", r)
			}
			// The load has ended, or its container is being stopped, before
			// the hold is released.
			cid := "ctr-" + sv.ID
			stop := "stop and remove " + sv.ID
			if tt.stopErr != nil {
				stop += " failed"
			}
			want := []string{"exec " + cid + " dump", "hold " + sv.ID, "exec " + cid + " restore aborted", stop, "release " + sv.ID}
			if got := e.rec.list(); !reflect.DeepEqual(got, want) {
				t.Errorf("events:\n got %q\nwant %q", got, want)
			}
			fs, stopped := e.fenceState(sv.ID)
			if !stopped || (len(fs) > 0) != tt.wantFenced {
				t.Fatalf("fences %+v, stopped %v; want stopped, fenced %v", fs, stopped, tt.wantFenced)
			}
			if !tt.wantFenced {
				return
			}
			// The next start stops the database, in case the load still runs.
			e.setRunning(sv, true)
			if err := e.m.Recover(e.ctx); err != nil {
				t.Fatal(err)
			}
			if c, _ := e.docker.Inspect(e.ctx, cid); c.Running {
				t.Error("Recover left the database running")
			}
			if fs, stopped := e.fenceState(sv.ID); len(fs) != 0 || !stopped {
				t.Errorf("after Recover: fences %+v, stopped %v; want none, stopped", fs, stopped)
			}
		})
	}
}

func TestRestoreDumpSuccessLiftsFence(t *testing.T) {
	e := newEnv(t)
	sv := e.service("mysql", true, "/var/lib/mysql")
	b := e.backUp(sv.ID)
	var fenced bool
	e.docker.restoreHook = func(context.Context) error {
		fs, stopped := e.fenceState(sv.ID)
		fenced = len(fs) == 1 && fs[0].Phase == store.RestoreLoading && stopped
		return nil
	}
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := mustLatestRestore(t, e, sv.ID); r.Status != store.RestoreSucceeded {
		t.Fatalf("restore = %+v", r)
	}
	if !fenced {
		t.Error("service not fenced while the dump loaded")
	}
	if fs, stopped := e.fenceState(sv.ID); len(fs) != 0 || stopped {
		t.Errorf("fences %+v, stopped %v; want none, running", fs, stopped)
	}
}

func TestRestoreRedis(t *testing.T) {
	e := newEnv(t)
	sv := e.service("redis", true, "/data")
	vols, _ := e.st.Volumes(e.ctx, sv.ID)
	e.docker.volumes["/data"] = volumeTar("/data", map[string]string{"dump.rdb": "REDIS-newer"})
	e.docker.dump = "REDIS0012..."
	b := e.backUp(sv.ID)
	e.rec = &recorder{}
	e.docker.rec, e.services.rec = e.rec, e.rec

	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r, _ := e.st.LatestRestore(e.ctx, sv.ID); r.Status != store.RestoreSucceeded {
		t.Fatalf("restore = %+v", r)
	}
	vol := volumeName(vols[0])
	events := e.rec.list()
	retained := slices.Index(events, "ensure volume "+vol+"-pre-restore")
	replaced := slices.Index(events, "create shed-restore-"+mustLatestRestore(t, e, sv.ID).ID+" img-redis "+vol+":/data:ro=false")
	if retained < 0 || replaced < retained {
		t.Errorf("volume not copied aside before it was replaced: %q", events)
	}
	got := readTar(t, bytes.NewReader(e.docker.extracted))
	if len(got) != 4 || got[0].name != "data/dump.rdb" || got[0].body != "REDIS0012..." {
		t.Errorf("extracted %+v", got)
	}
	want := map[string]string{"dump.rdb": "REDIS0012...", "appendonlydir/appendonly.aof.1.base.rdb": "REDIS0012...",
		"appendonlydir/appendonly.aof.manifest": redisManifestBody}
	if files := e.docker.volumeFiles(vol); !reflect.DeepEqual(files, want) {
		t.Errorf("volume holds %q, want %q", files, want)
	}
}

// fenceState returns the fences of the test service and whether it is
// stopped.
func (e *testEnv) fenceState(serviceID string) ([]store.RestoreFence, bool) {
	e.t.Helper()
	fs, err := e.st.RestoreFences(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	sv, err := e.st.Service(e.ctx, serviceID)
	if err != nil {
		e.t.Fatal(err)
	}
	return slices.DeleteFunc(fs, func(f store.RestoreFence) bool { return f.ServiceID != serviceID }), sv.Stopped
}

// failingRestore sets up an app with a backup of /srv holding f=original,
// whose current data is f=current, and queues a restore of the backup.
func failingRestore(t *testing.T) (*testEnv, store.Service, store.Restore) {
	t.Helper()
	e := newEnv(t)
	sv := e.service("app", true, "/srv")
	e.docker.volumes["/srv"] = volumeTar("/srv", map[string]string{"f": "original"})
	b := e.backUp(sv.ID)
	e.docker.volumes["/srv"] = volumeTar("/srv", map[string]string{"f": "current"})
	r, err := e.m.Restore(e.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	return e, sv, r
}

func TestRestoreFailurePutsBackPreviousData(t *testing.T) {
	tests := []struct {
		name  string
		setup func(e *testEnv, r store.Restore)
		want  string // in the restore's error
	}{
		{
			name: "extraction fails",
			setup: func(e *testEnv, r store.Restore) {
				e.docker.copyToHook = func(name string) error {
					if name == "shed-restore-"+r.ID {
						return errors.New("disk full")
					}
					return nil
				}
			},
			want: "disk full",
		},
		{
			name:  "extraction loses files",
			setup: func(e *testEnv, r store.Restore) { e.docker.lossy["shed-restore-"+r.ID] = true },
			want:  `1 of 2 entries are missing or differ, such as "srv/f"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, sv, r := failingRestore(t)
			tt.setup(e, r)
			e.drain()
			got := mustLatestRestore(t, e, sv.ID)
			if got.Status != store.RestoreFailed || !strings.Contains(got.Error, tt.want) ||
				!strings.Contains(got.Error, "the previous data was put back") {
				t.Errorf("restore = %+v", got)
			}
			vols, _ := e.st.Volumes(e.ctx, sv.ID)
			if files := e.docker.volumeFiles(volumeName(vols[0])); !reflect.DeepEqual(files, map[string]string{"f": "current"}) {
				t.Errorf("volume holds %q, want the previous data", files)
			}
			// The data is complete again, so the service starts.
			if fs, stopped := e.fenceState(sv.ID); len(fs) != 0 || stopped {
				t.Errorf("fences %+v, stopped %v; want none, running", fs, stopped)
			}
			if !slices.Contains(e.rec.list(), "remove volume "+preRestoreName(vols[0])) {
				t.Error("pre-restore volume not removed")
			}
			if e.services.released != 1 {
				t.Errorf("released %d times, want 1", e.services.released)
			}
		})
	}
}

func TestRestoreFailureKeepsServiceStopped(t *testing.T) {
	e, sv, r := failingRestore(t)
	dst := 0
	e.docker.copyToHook = func(name string) error {
		switch name {
		case "shed-restore-" + r.ID:
			return errors.New("disk full")
		case "shed-restore-" + r.ID + "-dst":
			if dst++; dst > 1 { // Copying aside works; putting back fails.
				return errors.New("i/o error")
			}
		}
		return nil
	}
	e.drain()
	got := mustLatestRestore(t, e, sv.ID)
	if got.Status != store.RestoreFailed || !strings.Contains(got.Error, "disk full") ||
		!strings.Contains(got.Error, "left stopped") || !strings.Contains(got.Error, "i/o error") {
		t.Errorf("restore = %+v", got)
	}
	fs, stopped := e.fenceState(sv.ID)
	if len(fs) != 1 || fs[0].Phase != store.RestoreReplacing || fs[0].RestoreID != r.ID || !stopped {
		t.Fatalf("fences %+v, stopped %v; want fenced and stopped", fs, stopped)
	}
	vols, _ := e.st.Volumes(e.ctx, sv.ID)
	// The pre-restore volume holds the only complete copy, so it is kept.
	if files := e.docker.volumeFiles(preRestoreName(vols[0])); !reflect.DeepEqual(files, map[string]string{"f": "current"}) {
		t.Errorf("pre-restore volume holds %q", files)
	}

	// A restart puts the previous data back before anything starts.
	e.docker.copyToHook = nil
	if err := e.m.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	if files := e.docker.volumeFiles(volumeName(vols[0])); !reflect.DeepEqual(files, map[string]string{"f": "current"}) {
		t.Errorf("volume holds %q after Recover, want the previous data", files)
	}
	if fs, stopped := e.fenceState(sv.ID); len(fs) != 0 || stopped {
		t.Errorf("after Recover: fences %+v, stopped %v; want none, running", fs, stopped)
	}
}

func TestRestoreInterruptedByShutdown(t *testing.T) {
	e, sv, r := failingRestore(t)
	e.docker.copyToHook = func(name string) error {
		if name == "shed-restore-"+r.ID {
			e.m.mu.Lock()
			e.m.running.cancel(nil)
			e.m.mu.Unlock()
			return context.Canceled
		}
		return nil
	}
	e.drain()
	// The volume may hold partial data, so the service stays stopped and the
	// previous data is put back on the next start.
	fs, stopped := e.fenceState(sv.ID)
	if len(fs) != 1 || fs[0].Phase != store.RestoreReplacing || !stopped {
		t.Fatalf("fences %+v, stopped %v; want fenced and stopped", fs, stopped)
	}
	if got := mustLatestRestore(t, e, sv.ID); got.Status != store.RestoreFailed {
		t.Errorf("restore = %+v", got)
	}
}

func TestRecoverFences(t *testing.T) {
	tests := []struct {
		name        string
		phase       store.RestorePhase
		wasStopped  bool
		userStarted bool
		failCopy    bool
		wantVolume  string // contents of f in the volume afterwards
		wantFenced  bool
		wantStopped bool
	}{
		// Retaining: the volume is unchanged and the copy is dropped.
		{name: "retaining", phase: store.RestoreRetaining, wantVolume: "partial"},
		{name: "retaining, stopped by the user", phase: store.RestoreRetaining, wasStopped: true, wantVolume: "partial", wantStopped: true},
		// Replacing: the previous data is put back.
		{name: "replacing", phase: store.RestoreReplacing, wantVolume: "previous"},
		{name: "replacing, put back fails", phase: store.RestoreReplacing, failCopy: true, wantVolume: "", wantFenced: true, wantStopped: true},
		// The user started the service since: its data stays.
		{name: "started by the user", phase: store.RestoreReplacing, userStarted: true, wantVolume: "partial"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			sv := e.service("app", false, "/srv")
			vols, _ := e.st.Volumes(e.ctx, sv.ID)
			vol, pre := volumeName(vols[0]), preRestoreName(vols[0])
			e.docker.volumes["/srv"] = volumeTar("/srv", map[string]string{"f": "partial"})
			// The pre-restore volume holds the previous data.
			e.docker.touched[pre] = true
			e.docker.data[pre] = map[string]fakeFile{"f": {hdr: tar.Header{Typeflag: tar.TypeReg, Mode: 0o644, Size: 8}, body: []byte("previous")}}
			if tt.wasStopped {
				e.st.SetServiceStopped(e.ctx, sv.ID, true)
			}
			if _, err := e.st.CreateRestoreFence(e.ctx, store.RestoreFence{
				ServiceID: sv.ID, RestoreID: "r1", Phase: tt.phase, Image: "img-app", VolumeIDs: []string{vols[0].ID},
			}); err != nil {
				t.Fatal(err)
			}
			if tt.userStarted {
				e.st.SetServiceStopped(e.ctx, sv.ID, false)
			}
			if tt.failCopy {
				e.docker.copyToErr = errors.New("i/o error")
			}
			if err := e.m.Recover(e.ctx); err != nil {
				t.Fatal(err)
			}
			e.docker.mu.Lock()
			f := string(e.docker.files(vol, "/srv")["f"].body)
			e.docker.mu.Unlock()
			if f != tt.wantVolume {
				t.Errorf("volume holds f=%q, want %q", f, tt.wantVolume)
			}
			fs, stopped := e.fenceState(sv.ID)
			if (len(fs) > 0) != tt.wantFenced || stopped != tt.wantStopped {
				t.Errorf("fences %+v, stopped %v; want fenced %v, stopped %v", fs, stopped, tt.wantFenced, tt.wantStopped)
			}
			removed := slices.Contains(e.rec.list(), "remove volume "+pre)
			if wantRemoved := !tt.wantFenced && !tt.userStarted; removed != wantRemoved {
				t.Errorf("pre-restore volume removed: %v, want %v", removed, wantRemoved)
			}
			if n := len(e.docker.mounts); n != 0 {
				t.Errorf("%d helper containers left", n)
			}
		})
	}
}

func mustLatestRestore(t *testing.T, e *testEnv, serviceID string) store.Restore {
	t.Helper()
	r, err := e.st.LatestRestore(e.ctx, serviceID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRestoreReleasesOnFailure(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv")
	e.docker.volumes["/srv"] = volumeTar("/srv", map[string]string{"f": "x"})
	b := e.backUp(sv.ID)
	e.docker.copyToErr = errors.New("disk full")
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	r := mustLatestRestore(t, e, sv.ID)
	if r.Status != store.RestoreFailed || !strings.Contains(r.Error, "disk full") {
		t.Errorf("restore = %+v, want failed", r)
	}
	if e.services.released != 1 {
		t.Errorf("released %d times, want 1", e.services.released)
	}
	if n := len(e.docker.containers); n != 0 {
		t.Errorf("%d containers left, want the helper removed", n)
	}
	// Copying the data aside failed, so the volume was never touched and
	// the service starts again.
	if slices.Contains(e.rec.list(), "remove volume shed-vol-"+mustVolume(t, e, sv.ID).ID) {
		t.Error("volume removed although copying it aside failed")
	}
	if fs, stopped := e.fenceState(sv.ID); len(fs) != 0 || stopped {
		t.Errorf("fences %+v, stopped %v; want none, running", fs, stopped)
	}
}

func mustVolume(t *testing.T, e *testEnv, serviceID string) store.Volume {
	t.Helper()
	vols, err := e.st.Volumes(e.ctx, serviceID)
	if err != nil || len(vols) == 0 {
		t.Fatalf("volumes = %v, %v", vols, err)
	}
	return vols[0]
}

func TestRestorePreRestoreFailure(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	b := e.backUp(sv.ID)
	e.docker.execErr = &docker.ExitError{Code: 2}
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	r := mustLatestRestore(t, e, sv.ID)
	if r.Status != store.RestoreFailed || !strings.Contains(r.Error, "pre-restore backup") {
		t.Errorf("restore = %+v, want failed by the pre-restore backup", r)
	}
	if slices.ContainsFunc(e.rec.list(), func(ev string) bool { return strings.HasPrefix(ev, "hold") }) {
		t.Errorf("service held although the pre-restore backup failed: %q", e.rec.list())
	}
}

func TestRestoreFromS3(t *testing.T) {
	e := newEnv(t)
	e.setS3("x")
	sv := e.service("mysql", true, "/var/lib/mysql")
	e.setPolicy(sv.ID, func(p *PolicyInput) { p.KeepLocal = 0 })
	e.docker.dump = "-- mysql"
	b := e.backUp(sv.ID)
	if b.Local {
		t.Fatalf("backup = %+v, want only remote", b)
	}
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := mustLatestRestore(t, e, sv.ID); r.Status != store.RestoreSucceeded {
		t.Fatalf("restore = %+v", r)
	}
	if e.docker.restored != "-- mysql" {
		t.Errorf("restored %q", e.docker.restored)
	}
	if p := e.partials(); len(p) > 0 {
		t.Errorf("download left behind: %v", p)
	}
}

func TestRestoreRejects(t *testing.T) {
	e := newEnv(t)
	sys := e.backUp("")
	if _, err := e.m.Restore(e.ctx, sys.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("Restore of shed.db = %v, want ErrInvalid", err)
	}
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.execErr = errors.New("boom")
	failed, _ := e.m.BackUp(e.ctx, sv.ID)
	e.drain()
	if _, err := e.m.Restore(e.ctx, failed.ID); !errors.Is(err, ErrInvalid) {
		t.Errorf("Restore of a failed backup = %v, want ErrInvalid", err)
	}
	e.docker.execErr = nil
	b := e.backUp(sv.ID)
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Restore(e.ctx, b.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("second Restore = %v, want ErrBusy", err)
	}
	if _, err := e.m.BackUp(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("BackUp during a restore = %v, want ErrBusy", err)
	}
	if err := e.m.Delete(e.ctx, b.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("Delete of a backup being restored = %v, want ErrBusy", err)
	}
}

func TestDelete(t *testing.T) {
	e := newEnv(t)
	e.setS3("")
	sv := e.service("postgres", true, "/var/lib/postgresql")
	b := e.backUp(sv.ID)
	if err := e.m.Delete(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.Backup(e.ctx, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("record still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, sv.ID, b.File)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("local file still exists: %v", err)
	}
	if k := e.remote.keys(); len(k) != 0 {
		t.Errorf("objects left: %v", k)
	}

	q, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.Delete(e.ctx, q.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("Delete of a queued backup = %v, want ErrBusy", err)
	}
}

func TestOpenDecrypts(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	sv := e.service("mongo", true, "/data/db")
	e.docker.dump = "mongo archive"
	b := e.backUp(sv.ID)
	if !b.Encrypted || b.File != b.ID+".archive.zst.age" {
		t.Fatalf("backup = %+v, want encrypted", b)
	}
	if got := e.content(b); got != "mongo archive" {
		t.Errorf("Open = %q", got)
	}
	// Turning encryption off keeps the identity for old archives.
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{}); err != nil {
		t.Fatal(err)
	}
	if got := e.content(b); got != "mongo archive" {
		t.Errorf("Open after disabling encryption = %q", got)
	}
}

func TestDownloadName(t *testing.T) {
	at := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		file, service, want string
	}{
		{"abc.sql.zst", "postgres", "postgres-20261004-030000.sql.zst"},
		{"abc.sql.zst.age", "db", "db-20261004-030000.sql.zst"},
		{"abc.db.zst.age", "", "shed-20261004-030000.db.zst"},
		{"abc.tar.zst", "web", "web-20261004-030000.tar.zst"},
	}
	for _, tt := range tests {
		if got := DownloadName(store.Backup{File: tt.file, CreatedAt: at}, tt.service); got != tt.want {
			t.Errorf("DownloadName(%s, %s) = %s, want %s", tt.file, tt.service, got, tt.want)
		}
	}
}

func TestSettings(t *testing.T) {
	e := newEnv(t)
	got, err := e.m.Settings(e.ctx)
	if err != nil || got.S3 != nil || got.Encrypt || got.Recipient != "" {
		t.Fatalf("initial Settings = %+v, %v", got, err)
	}
	if _, err := e.m.Identity(e.ctx); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Identity without a key = %v, want ErrNotFound", err)
	}

	in := S3Input{Endpoint: "https://s3.example.com", Region: "eu", Bucket: "b", Prefix: "/p/", AccessKeyID: "AK", SecretAccessKey: "SECRET"}
	got, err = e.m.SetSettings(e.ctx, SettingsInput{S3: &in, Encrypt: true})
	if err != nil {
		t.Fatal(err)
	}
	wantS3 := S3Settings{Endpoint: in.Endpoint, Region: "eu", Bucket: "b", Prefix: "p", AccessKeyID: "AK", HasSecret: true}
	if got.S3 == nil || *got.S3 != wantS3 || !got.Encrypt || !strings.HasPrefix(got.Recipient, "age1") {
		t.Errorf("Settings = %+v", got)
	}
	key, err := e.m.Identity(e.ctx)
	if err != nil || !strings.HasPrefix(key, "AGE-SECRET-KEY-1") {
		t.Errorf("Identity = %q, %v", key, err)
	}

	// An empty secret keeps the stored one, for saving and for testing.
	in.SecretAccessKey, in.Bucket = "", "b2"
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{S3: &in, Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.m.TestS3(e.ctx, in); err != nil {
		t.Fatal(err)
	}
	s, err := e.m.destination(e.ctx)
	if err != nil || s.SecretAccessKey != "SECRET" || s.Bucket != "b2" {
		t.Errorf("stored S3 = %+v, %v; want the old secret kept", s, err)
	}
	e.mu.Lock()
	last := e.configs[len(e.configs)-1]
	e.mu.Unlock()
	if last.SecretAccessKey != "SECRET" {
		t.Errorf("TestS3 used secret %q", last.SecretAccessKey)
	}
	if len(e.remote.checked) != 1 || !strings.HasPrefix(e.remote.checked[0], "p/.shed-check-") {
		t.Errorf("checked keys = %q", e.remote.checked)
	}
	e.remote.putErr = errors.New("access denied")
	if err := e.m.TestS3(e.ctx, in); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "access denied") {
		t.Errorf("failing TestS3 = %v", err)
	}

	// The identity survives turning encryption off and on again.
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.m.Identity(e.ctx); again != key {
		t.Error("the identity changed")
	}

	bad := S3Input{Endpoint: "https://s3.example.com", AccessKeyID: "AK", SecretAccessKey: "S"}
	if _, err := e.m.SetSettings(e.ctx, SettingsInput{S3: &bad}); !errors.Is(err, ErrInvalid) {
		t.Errorf("SetSettings without a bucket = %v, want ErrInvalid", err)
	}
}

// racingStore stores an age identity right after the first time the
// identity is found missing, like a concurrent request would.
type racingStore struct {
	*store.Store
	once  sync.Once
	other string
}

func (s *racingStore) Setting(ctx context.Context, key string) (string, error) {
	v, err := s.Store.Setting(ctx, key)
	if key == keyAgeIdentity && errors.Is(err, store.ErrNotFound) {
		s.once.Do(func() {
			if err := s.Store.SetSetting(ctx, keyAgeIdentity, s.other); err != nil {
				panic(err)
			}
		})
	}
	return v, err
}

func TestSetSettingsKeepsConcurrentIdentity(t *testing.T) {
	e := newEnv(t)
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	e.m.store = &racingStore{Store: e.st, other: other.String()}
	got, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true})
	if err != nil {
		t.Fatal(err)
	}
	if key, _ := e.m.Identity(e.ctx); key != other.String() {
		t.Error("SetSettings replaced the identity another request stored")
	}
	if got.Recipient != other.Recipient().String() {
		t.Errorf("Recipient = %s, want the stored identity's", got.Recipient)
	}
}

func TestSetSettingsConcurrentEncryption(t *testing.T) {
	e := newEnv(t)
	const n = 8
	recipients := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			s, err := e.m.SetSettings(e.ctx, SettingsInput{Encrypt: true})
			if err != nil {
				t.Error(err)
			}
			recipients[i] = s.Recipient
		})
	}
	wg.Wait()
	key, err := e.m.Identity(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := age.ParseX25519Identity(key)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range recipients {
		if r != id.Recipient().String() {
			t.Errorf("request %d encrypts to %s, but the stored identity is %s", i, r, id.Recipient())
		}
	}
}

func TestPolicyDefaultsAndStorage(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	for _, id := range []string{sv.ID, ""} {
		p, err := e.m.Policy(e.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		next := time.Date(2026, 10, 4, 3, 0, 0, 0, time.UTC)
		if p.PolicyInput != defaultPolicy || p.NextRun == nil || !p.NextRun.Equal(next) {
			t.Errorf("Policy(%q) = %+v, want the default running at %v", id, p, next)
		}
		in := PolicyInput{Enabled: true, Schedule: "30 4 * * *", Compression: "fastest", KeepLocal: 2, KeepRemote: 3, Upload: true}
		p, err = e.m.SetPolicy(e.ctx, id, in)
		if err != nil {
			t.Fatal(err)
		}
		next = time.Date(2026, 10, 4, 4, 30, 0, 0, time.UTC)
		if p.PolicyInput != in || p.NextRun == nil || !p.NextRun.Equal(next) {
			t.Errorf("SetPolicy(%q) = %+v", id, p)
		}
		in.Enabled = false
		if p, err = e.m.SetPolicy(e.ctx, id, in); err != nil || p.NextRun != nil {
			t.Errorf("disabled policy = %+v, %v; want no next run", p, err)
		}
		if p, _ := e.m.Policy(e.ctx, id); p.PolicyInput != in {
			t.Errorf("stored policy = %+v, want %+v", p, in)
		}
	}
	if _, err := e.m.SetPolicy(e.ctx, sv.ID, PolicyInput{Schedule: "nope"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("SetPolicy of an invalid policy = %v", err)
	}
	if _, err := e.m.Policy(e.ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Policy of an unknown service = %v", err)
	}
}

func TestSchedule(t *testing.T) {
	e := newEnv(t)
	db := e.service("postgres", true, "/var/lib/postgresql")
	app := e.service("app", true, "/srv")
	e.setPolicy(app.ID, func(p *PolicyInput) { p.Enabled = false })
	e.service("redis", true) // No volume: not backed up.
	undeployed, _ := e.st.CreateService(e.ctx, store.Service{ProjectID: e.project, Name: "u", Kind: "mysql"})
	e.st.CreateVolume(e.ctx, undeployed.ID, "/var/lib/mysql")
	e.setPolicy("", func(p *PolicyInput) { p.Schedule = "CRON_TZ=Europe/Paris 0 5 * * *" }) // 03:00 UTC

	queued := func() map[string]int {
		counts := map[string]int{}
		for _, id := range []string{db.ID, app.ID, undeployed.ID, ""} {
			bs, _ := e.st.Backups(e.ctx, id, 0)
			for _, b := range bs {
				if b.Trigger == store.BackupSchedule {
					counts[id]++
				}
			}
		}
		return counts
	}

	e.m.tick(e.ctx, time.Date(2026, 10, 4, 2, 59, 31, 0, time.UTC))
	if got := queued(); len(got) != 0 {
		t.Errorf("before 03:00 queued %v", got)
	}
	e.m.tick(e.ctx, time.Date(2026, 10, 4, 3, 0, 1, 0, time.UTC))
	if got, want := queued(), map[string]int{db.ID: 1, "": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("at 03:00 queued %v, want %v", got, want)
	}
	e.drain()
	e.m.tick(e.ctx, time.Date(2026, 10, 4, 3, 1, 1, 0, time.UTC))
	e.m.tick(e.ctx, time.Date(2026, 10, 4, 14, 0, 0, 0, time.UTC))
	if got, want := queued(), map[string]int{db.ID: 1, "": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("later the same day queued %v, want %v", got, want)
	}
	p, _ := e.m.Policy(e.ctx, db.ID)
	if want := time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC); p.NextRun == nil || !p.NextRun.Equal(want) {
		t.Errorf("next run = %v, want %v", p.NextRun, want)
	}

	// Runs missed while shed was down are skipped: a new Manager starts
	// counting from its first look at the schedule.
	e.m = New(Config{Store: e.st, Docker: e.docker, Services: e.services, Dir: e.dir, Now: e.clock.Now})
	e.m.tick(e.ctx, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))
	if got, want := queued(), map[string]int{db.ID: 1, "": 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("after a restart queued %v, want %v", got, want)
	}

	// A busy target skips its run.
	if _, err := e.m.BackUp(e.ctx, db.ID); err != nil {
		t.Fatal(err)
	}
	e.m.tick(e.ctx, time.Date(2026, 10, 7, 3, 0, 1, 0, time.UTC))
	if got, want := queued(), map[string]int{db.ID: 1, "": 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("while busy queued %v, want %v", got, want)
	}
}

func TestForgetService(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	other := e.service("redis", true, "/data")
	e.backUp(sv.ID)
	queued, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := e.m.BackUp(e.ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.ForgetService(e.ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if b := e.backup(queued.ID); b.Status != store.BackupFailed || b.Error != "service deleted" {
		t.Errorf("queued backup = %+v, want failed", b)
	}
	if _, err := os.Stat(filepath.Join(e.dir, sv.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("backup directory still exists: %v", err)
	}
	e.drain()
	if b := e.backup(keep.ID); b.Status != store.BackupSucceeded {
		t.Errorf("other service's backup = %+v", b)
	}
}

// blockingDocker blocks dumps until their context ends.
type blockingDocker struct {
	*fakeDocker
	started chan struct{}
}

func (b *blockingDocker) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	io.WriteString(stdout, "partial output")
	close(b.started)
	<-ctx.Done()
	return ctx.Err()
}

func newBlockingEnv(t *testing.T) (*testEnv, *blockingDocker) {
	e := newEnv(t)
	bd := &blockingDocker{fakeDocker: e.docker, started: make(chan struct{})}
	e.m = New(Config{Store: e.st, Docker: bd, Services: e.services, Dir: e.dir, Now: e.clock.Now})
	return e, bd
}

func TestForgetServiceCancelsRunningJob(t *testing.T) {
	e, bd := newBlockingEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { e.m.Run(ctx) })
	defer wg.Wait()
	defer cancel()

	b, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-bd.started
	if err := e.m.ForgetService(e.ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.backup(b.ID); got.Status != store.BackupFailed || got.Error != "service deleted" {
		t.Errorf("backup = %+v, want failed by the deletion", got)
	}
	if p := e.partials(); len(p) > 0 {
		t.Errorf("partial files left: %v", p)
	}
}

func TestRunShutdown(t *testing.T) {
	e, bd := newBlockingEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	ctx, cancel := context.WithCancel(e.ctx)
	stopped := make(chan struct{})
	go func() {
		e.m.Run(ctx)
		close(stopped)
	}()

	running, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-bd.started
	queued, err := e.m.BackUp(e.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-stopped
	for _, id := range []string{running.ID, queued.ID} {
		if b := e.backup(id); b.Status != store.BackupFailed || b.Error != "interrupted by shutdown" {
			t.Errorf("backup = %+v, want interrupted by shutdown", b)
		}
	}
	if _, err := e.m.BackUp(e.ctx, ""); !errors.Is(err, ErrStopped) {
		t.Errorf("BackUp after shutdown = %v, want ErrStopped", err)
	}
}

func TestRecover(t *testing.T) {
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	running, _ := e.st.CreateBackup(e.ctx, store.Backup{ServiceID: sv.ID, Trigger: store.BackupManual, Method: store.MethodDump, Status: store.BackupRunning})
	queued, _ := e.st.CreateBackup(e.ctx, store.Backup{Trigger: store.BackupSchedule, Method: store.MethodSQLite, Status: store.BackupQueued})
	r, _ := e.st.CreateRestore(e.ctx, store.Restore{ServiceID: sv.ID, BackupID: running.ID, Status: store.RestoreRunning})

	dir := filepath.Join(e.dir, sv.ID)
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "x.sql.zst.partial"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "y.sql.zst"), nil, 0o600)
	e.docker.containers["h"] = docker.Container{ID: "h", Labels: map[string]string{helperLabel: "x"}}
	e.docker.containers["app"] = docker.Container{ID: "app", Labels: map[string]string{"shed.service": sv.ID}}

	if err := e.m.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{running.ID, queued.ID} {
		if b := e.backup(id); b.Status != store.BackupFailed || b.Error != "interrupted by restart" {
			t.Errorf("backup = %+v", b)
		}
	}
	if got := mustLatestRestore(t, e, sv.ID); got.ID != r.ID || got.Status != store.RestoreFailed {
		t.Errorf("restore = %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "y.sql.zst" {
		t.Errorf("files after Recover = %v", entries)
	}
	if _, ok := e.docker.containers["h"]; ok {
		t.Error("helper container not removed")
	}
	if _, ok := e.docker.containers["app"]; !ok {
		t.Error("service container removed")
	}

	// A missing directory is fine.
	e2 := newEnv(t)
	if err := e2.m.Recover(e2.ctx); err != nil {
		t.Error(err)
	}
}

func TestVolumeArchiveRoundTrip(t *testing.T) {
	// A volume archive written by a backup restores to the same entries.
	e := newEnv(t)
	sv := e.service("app", true, "/srv/data")
	var src bytes.Buffer
	tw := tar.NewWriter(&src)
	tw.WriteHeader(&tar.Header{Name: "data/", Typeflag: tar.TypeDir, Mode: 0o750, Uid: 1000})
	tw.WriteHeader(&tar.Header{Name: "data/f", Typeflag: tar.TypeReg, Mode: 0o600, Uid: 1000, Size: 2})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "data/l", Typeflag: tar.TypeSymlink, Linkname: "f"})
	tw.Close()
	e.docker.volumes["/srv/data"] = src.Bytes()
	b := e.backUp(sv.ID)
	if _, err := e.m.Restore(e.ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()
	if r := mustLatestRestore(t, e, sv.ID); r.Status != store.RestoreSucceeded {
		t.Fatalf("restore = %+v", r)
	}
	got := readTar(t, bytes.NewReader(e.docker.extracted))
	want := []entry{
		{name: "srv/data/", typ: tar.TypeDir, mode: 0o750, uid: 1000},
		{name: "srv/data/f", typ: tar.TypeReg, mode: 0o600, uid: 1000, body: "hi"},
		{name: "srv/data/l", typ: tar.TypeSymlink, link: "f"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extracted:\n got %+v\nwant %+v", got, want)
	}
}

func TestHelperRemovesAnonymousVolumes(t *testing.T) {
	e := newEnv(t)
	e.docker.imageVolume = true
	sv := e.service("mongo", false, "/data/db")
	e.docker.volumes["/data/db"] = volumeTar("/data/db", nil)
	e.backUp(sv.ID)
	if len(e.docker.anonymous) != 0 {
		t.Errorf("anonymous volumes left after the backup: %v", e.docker.anonymous)
	}
	if events := e.rec.list(); slices.ContainsFunc(events, func(ev string) bool { return strings.HasPrefix(ev, "remove volume shed-vol-") }) {
		t.Errorf("a service volume was removed: %q", events)
	}

	// Recover cleans up leftover helpers the same way.
	e.docker.containers["h"] = docker.Container{ID: "h", Labels: map[string]string{helperLabel: "x"}, Volumes: []string{"shed-vol-abc", "anon-h"}}
	e.docker.anonymous["anon-h"] = true
	if err := e.m.Recover(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.docker.containers["h"]; ok || len(e.docker.anonymous) != 0 {
		t.Errorf("after Recover: containers %v, anonymous volumes %v", e.docker.containers, e.docker.anonymous)
	}
}

// failingStore fails deleting and updating the backups in fail.
type failingStore struct {
	Store
	fail map[string]bool
}

func (s failingStore) DeleteBackup(ctx context.Context, id string) error {
	if s.fail[id] {
		return errors.New("delete " + id + " failed")
	}
	return s.Store.DeleteBackup(ctx, id)
}

func (s failingStore) UpdateBackup(ctx context.Context, b store.Backup) error {
	if s.fail[b.ID] {
		return errors.New("update " + b.ID + " failed")
	}
	return s.Store.UpdateBackup(ctx, b)
}

func TestPruneReportsEachFailureOnce(t *testing.T) {
	tests := []struct {
		name string
		fail []int // indexes into the backups, newest first
		want []string
	}{
		{name: "none"},
		{name: "one delete", fail: []int{1}, want: []string{"delete %s failed"}},
		{name: "last delete before an exempt backup", fail: []int{2}, want: []string{"delete %s failed"}},
		{name: "two deletes", fail: []int{1, 2}, want: []string{"delete %s failed", "delete %s failed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			sv := e.service("postgres", true, "/var/lib/postgresql")
			// Newest first: a kept backup, two to drop, and an exempt manual
			// backup after them, which a stale error would be repeated for.
			specs := []struct {
				trigger store.BackupTrigger
				age     int
			}{{store.BackupSchedule, 0}, {store.BackupSchedule, 1}, {store.BackupSchedule, 2}, {store.BackupManual, 3}}
			var ids []string
			for _, sp := range specs {
				b, err := e.st.CreateBackup(e.ctx, store.Backup{
					ServiceID: sv.ID, Trigger: sp.trigger, Method: store.MethodDump, Status: store.BackupSucceeded,
					File: store.NewID() + ".sql.zst", Local: true,
					CreatedAt: time.Date(2026, 10, 4-sp.age, 3, 0, 0, 0, time.UTC),
				})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, b.ID)
			}
			fail := make(map[string]bool)
			var want []string
			for i, idx := range tt.fail {
				fail[ids[idx]] = true
				want = append(want, fmt.Sprintf(tt.want[i], ids[idx]))
			}
			m := New(Config{Store: failingStore{e.st, fail}, Docker: e.docker, Services: e.services, Dir: e.dir, Now: e.clock.Now})

			err := m.prune(e.ctx, sv.ID, PolicyInput{KeepLocal: 1})
			var got []string
			if err != nil {
				got = strings.Split(err.Error(), "\n")
			}
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("prune errors = %q, want %q", got, want)
			}
		})
	}
}

// blockingCopyDocker blocks reads of volumes. They end when their context
// does, or, with ignoreCtx, only when release is closed.
type blockingCopyDocker struct {
	*fakeDocker
	started           chan struct{}
	release           chan struct{}
	ignoreCtx         bool
	once, releaseOnce sync.Once
}

func (b *blockingCopyDocker) CopyFrom(ctx context.Context, id, p string) (io.ReadCloser, error) {
	b.once.Do(func() { close(b.started) })
	if b.ignoreCtx {
		<-b.release
	} else {
		<-ctx.Done()
	}
	return nil, ctx.Err()
}

// startBlocked replaces e.m with a Manager over the same store and
// directory whose volume reads block, and starts its worker. The worker
// stops, and the test waits for it, when the test ends.
func startBlocked(e *testEnv, ignoreCtx bool) *blockingCopyDocker {
	bd := &blockingCopyDocker{fakeDocker: e.docker, started: make(chan struct{}), release: make(chan struct{}), ignoreCtx: ignoreCtx}
	e.m = New(Config{Store: e.st, Docker: bd, Services: e.services, Dir: e.dir, Now: e.clock.Now})
	ctx, cancel := context.WithCancel(e.ctx)
	var wg sync.WaitGroup
	wg.Go(func() { e.m.work(ctx) })
	e.t.Cleanup(func() {
		cancel()
		bd.releaseOnce.Do(func() { close(bd.release) })
		wg.Wait()
	})
	return bd
}

func (e *testEnv) helpers() int {
	e.t.Helper()
	cs, err := e.docker.List(e.ctx, map[string]string{helperLabel: ""})
	if err != nil {
		e.t.Fatal(err)
	}
	return len(cs)
}

func (e *testEnv) pausedCount(serviceID string) int {
	e.m.mu.Lock()
	defer e.m.mu.Unlock()
	return e.m.paused[serviceID]
}

func TestPauseServiceCancelsBackups(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv/a")
	e.docker.volumes["/srv/a"] = volumeTar("/srv/a", map[string]string{"x": "1"})
	done := e.backUp(sv.ID)
	bd := startBlocked(e, false)

	running, err := e.m.BackUp(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-bd.started
	r, err := e.st.CreateRestore(e.ctx, store.Restore{ServiceID: sv.ID, BackupID: done.ID, Status: store.RestoreRunning})
	if err != nil {
		t.Fatal(err)
	}
	e.m.mu.Lock()
	e.m.push(&job{serviceID: sv.ID, backup: done, restore: &r})
	e.m.mu.Unlock()

	resume, err := e.m.PauseService(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.backup(running.ID); got.Status != store.BackupFailed || got.Error != errPaused.Error() {
		t.Errorf("running backup = %+v, want failed with %q", got, errPaused)
	}
	if got := mustLatestRestore(t, e, sv.ID); got.Status != store.RestoreFailed || got.Error != errPaused.Error() {
		t.Errorf("queued restore = %+v, want failed with %q", got, errPaused)
	}
	if n := e.helpers(); n != 0 {
		t.Errorf("%d helper containers left when PauseService returned", n)
	}

	// New work is rejected, scheduled backups are skipped.
	if _, err := e.m.BackUp(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("BackUp while paused = %v, want ErrBusy", err)
	}
	if _, err := e.m.Restore(e.ctx, done.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("Restore while paused = %v, want ErrBusy", err)
	}
	e.m.tick(e.ctx, time.Date(2026, 10, 4, 2, 59, 31, 0, time.UTC))
	e.m.tick(e.ctx, time.Date(2026, 10, 4, 3, 0, 1, 0, time.UTC))
	bs, _ := e.st.Backups(e.ctx, sv.ID, 0)
	for _, b := range bs {
		if b.Trigger == store.BackupSchedule {
			t.Errorf("scheduled backup %+v ran while paused", b)
		}
	}

	// A second pause keeps the service paused until both resume.
	resume2, err := e.m.PauseService(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume()
	resume()
	if _, err := e.m.BackUp(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Errorf("BackUp with one pause left = %v, want ErrBusy", err)
	}
	resume2()
	if n := e.pausedCount(sv.ID); n != 0 {
		t.Errorf("paused count = %d after resuming, want 0", n)
	}
	if _, err := e.m.BackUp(e.ctx, sv.ID); err != nil {
		t.Errorf("BackUp after resume = %v", err)
	}
}

func TestPauseServiceKeepsRunningRestore(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv/a")
	e.docker.volumes["/srv/a"] = volumeTar("/srv/a", map[string]string{"x": "1"})
	b := e.backUp(sv.ID)
	bd := startBlocked(e, false)

	r, err := e.m.Restore(e.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	<-bd.started // Blocked in the pre-restore backup.
	if _, err := e.m.PauseService(e.ctx, sv.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("PauseService during a restore = %v, want ErrBusy", err)
	}
	if n := e.pausedCount(sv.ID); n != 0 {
		t.Errorf("paused count = %d, want 0", n)
	}
	got, err := e.st.LatestRestore(e.ctx, sv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != r.ID || got.Status != store.RestoreRunning {
		t.Errorf("restore = %+v, want still running", got)
	}
	e.m.mu.Lock()
	canceled := e.m.running.ctx.Err()
	e.m.mu.Unlock()
	if canceled != nil {
		t.Errorf("running restore was canceled: %v", canceled)
	}
}

func TestPauseServiceContext(t *testing.T) {
	e := newEnv(t)
	sv := e.service("app", true, "/srv/a")
	bd := startBlocked(e, true)
	if _, err := e.m.BackUp(e.ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	<-bd.started

	ctx, cancel := context.WithTimeout(e.ctx, 50*time.Millisecond)
	defer cancel()
	resume, err := e.m.PauseService(ctx, sv.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PauseService = %v, want the context's error", err)
	}
	if resume != nil {
		t.Error("PauseService returned a resume function with its error")
	}
	if n := e.pausedCount(sv.ID); n != 0 {
		t.Errorf("paused count = %d after the context ended, want 0", n)
	}
}
