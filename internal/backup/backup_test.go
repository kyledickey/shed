package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	want := []string{
		"hold " + sv.ID,
		"stop and remove " + sv.ID,
		"remove volume " + volName["/srv/data"],
		"ensure volume " + volName["/srv/data"],
		"remove volume " + volName["/srv/cache"],
		"ensure volume " + volName["/srv/cache"],
		"create shed-restore-" + r.ID + " img-app " + volName["/srv/data"] + ":/srv/data:ro=false," + volName["/srv/cache"] + ":/srv/cache:ro=false",
		"copy to helper3 /",
		"remove helper3",
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

func TestRestoreRedis(t *testing.T) {
	e := newEnv(t)
	sv := e.service("redis", true, "/data")
	vols, _ := e.st.Volumes(e.ctx, sv.ID)
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
	want := []string{
		"hold " + sv.ID, "stop and remove " + sv.ID, "remove volume " + vol, "ensure volume " + vol,
		"create shed-restore-" + mustLatestRestore(t, e, sv.ID).ID + " img-redis " + vol + ":/data:ro=false",
	}
	if got := events[1 : 1+len(want)]; !reflect.DeepEqual(got, want) {
		t.Errorf("events:\n got %q\nwant %q", events, want)
	}
	got := readTar(t, bytes.NewReader(e.docker.extracted))
	if len(got) != 4 || got[0].name != "data/dump.rdb" || got[0].body != "REDIS0012..." {
		t.Errorf("extracted %+v", got)
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
	s, err := e.m.loadS3(e.ctx)
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
