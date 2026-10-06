//go:build integration

package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/catalog"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// itEnv is a Manager running against the real Docker daemon.
type itEnv struct {
	t       *testing.T
	ctx     context.Context
	st      *store.Store
	dc      *docker.Client
	m       *Manager
	project string
}

func newITEnv(t *testing.T) *itEnv {
	t.Helper()
	dc, err := docker.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dc.Close() })
	ctx := context.Background()
	if err := dc.Ping(ctx); err != nil {
		t.Skipf("no Docker daemon: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p, err := st.CreateProject(ctx, "it-"+store.NewID())
	if err != nil {
		t.Fatal(err)
	}
	e := &itEnv{t: t, ctx: ctx, st: st, dc: dc, project: p.ID}
	e.m = New(Config{
		Store:    st,
		Docker:   dc,
		Services: &itServices{e: e, specs: make(map[string]docker.RunSpec)},
		Dir:      t.TempDir(),
		Log:      slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if _, err := e.m.SetSettings(ctx, SettingsInput{Encrypt: true}); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() { e.m.Run(runCtx) })
	t.Cleanup(func() { cancel(); wg.Wait() })
	return e
}

// itServices holds services by stopping and recreating their containers
// directly, like the deployer does.
type itServices struct {
	e     *itEnv
	mu    sync.Mutex
	specs map[string]docker.RunSpec // by service ID
}

func (s *itServices) Hold(_ context.Context, id string) (Held, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &itHeld{s: s, id: id, spec: s.specs[id]}, nil
}

type itHeld struct {
	s    *itServices
	id   string
	spec docker.RunSpec
}

func (h *itHeld) Active(ctx context.Context) (store.Deployment, error) {
	return h.s.e.st.ActiveDeployment(ctx, h.id)
}

func (h *itHeld) Running(ctx context.Context) (bool, error) {
	d, err := h.Active(ctx)
	if err != nil {
		return false, err
	}
	c, err := h.s.e.dc.Inspect(ctx, d.ContainerID)
	if docker.IsNotFound(err) {
		return false, nil
	}
	return c.Running, err
}

func (h *itHeld) StopAndRemove(ctx context.Context) error {
	d, err := h.Active(ctx)
	if err != nil {
		return err
	}
	return h.s.e.dc.Remove(ctx, d.ContainerID)
}

func (h *itHeld) Release(ctx context.Context) error {
	d, err := h.Active(ctx)
	if err != nil {
		return err
	}
	c, err := h.s.e.dc.Inspect(ctx, d.ContainerID)
	switch {
	case docker.IsNotFound(err):
		id, err := h.s.e.dc.Run(ctx, h.spec)
		if err != nil {
			return err
		}
		d.ContainerID = id
		return h.s.e.st.UpdateDeployment(ctx, d)
	case err != nil:
		return err
	case !c.Running:
		return h.s.e.dc.Start(ctx, d.ContainerID)
	}
	return nil
}

// itService is a service whose active deployment runs a real container.
type itService struct {
	sv  store.Service
	vol store.Volume
}

// startService creates a service of kind with one volume at mountPath and
// runs its container.
func (e *itEnv) startService(kind, image string, env []string, cmd []string, mountPath string) itService {
	t := e.t
	t.Helper()
	ctx := e.ctx
	if err := e.dc.PullImage(ctx, image, io.Discard); err != nil {
		t.Fatal(err)
	}
	sv, err := e.st.CreateService(ctx, store.Service{ProjectID: e.project, Name: kind + "-" + store.NewID()[:4], Kind: kind, Image: image})
	if err != nil {
		t.Fatal(err)
	}
	vol, err := e.st.CreateVolume(ctx, sv.ID, mountPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.dc.EnsureVolume(ctx, volumeName(vol)); err != nil {
		t.Fatal(err)
	}
	spec := docker.RunSpec{
		Name:   "shed-it-" + sv.ID,
		Image:  image,
		Cmd:    cmd,
		Env:    env,
		Labels: map[string]string{"shed.it": sv.ID},
		Mounts: []docker.Mount{{Volume: volumeName(vol), Target: mountPath}},
	}
	e.m.services.(*itServices).specs[sv.ID] = spec
	t.Cleanup(func() {
		ctx := context.Background()
		cs, _ := e.dc.List(ctx, map[string]string{"shed.it": sv.ID})
		helpers, _ := e.dc.List(ctx, map[string]string{helperLabel: ""})
		if len(helpers) > 0 {
			t.Errorf("helper containers left: %v", helpers)
		}
		for _, c := range append(cs, helpers...) {
			e.dc.Remove(ctx, c.ID) // Also removes the image's anonymous volumes.
		}
		if err := e.dc.RemoveVolume(ctx, volumeName(vol)); err != nil {
			t.Errorf("remove volume: %v", err)
		}
	})
	id, err := e.dc.Run(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.CreateDeployment(ctx, store.Deployment{
		ServiceID: sv.ID, Status: store.StatusActive, Trigger: store.TriggerCreate, Image: image, ContainerID: id,
	}); err != nil {
		t.Fatal(err)
	}
	return itService{sv: sv, vol: vol}
}

// catalogService starts a database from its catalog template.
func (e *itEnv) catalogService(kind string) itService {
	tpl, ok := catalog.Lookup(kind)
	if !ok {
		e.t.Fatalf("no template %s", kind)
	}
	var env []string
	for k, v := range tpl.Vars() {
		if !strings.Contains(v, "${{") {
			env = append(env, k+"="+v)
		}
	}
	return e.startService(kind, tpl.Image, env, tpl.Cmd, tpl.MountPath)
}

// container returns the current container of s.
func (e *itEnv) container(s itService) string {
	d, err := e.st.ActiveDeployment(e.ctx, s.sv.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return d.ContainerID
}

// sh runs a script in the service's container and returns its output.
func (e *itEnv) sh(s itService, script string) (string, error) {
	var out, errOut bytes.Buffer
	err := e.dc.Exec(e.ctx, e.container(s), []string{"sh", "-c", script}, nil, &out, &errOut)
	if err != nil {
		return "", fmt.Errorf("%w: %s%s", err, out.String(), errOut.String())
	}
	return strings.TrimSpace(out.String()), nil
}

func (e *itEnv) mustSh(s itService, script string) string {
	e.t.Helper()
	out, err := e.sh(s, script)
	if err != nil {
		e.t.Fatalf("%s: %v", script, err)
	}
	return out
}

// waitReady waits until PID 1 of the container is the server binary, past
// the image's initialization, and ping succeeds.
func (e *itEnv) waitReady(s itService, binary, ping string) {
	e.t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	script := fmt.Sprintf(`case "$(tr '\0' ' ' </proc/1/cmdline | cut -d' ' -f1)" in *%s) ;; *) exit 1 ;; esac; %s`, binary, ping)
	var err error
	for time.Now().Before(deadline) {
		if _, err = e.sh(s, script); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	e.t.Fatalf("%s not ready: %v", binary, err)
}

// backUp runs a manual backup to completion.
func (e *itEnv) backUp(s itService) store.Backup {
	e.t.Helper()
	b, err := e.m.BackUp(e.ctx, s.sv.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	for range 600 {
		got, err := e.st.Backup(e.ctx, b.ID)
		if err != nil {
			e.t.Fatal(err)
		}
		switch got.Status {
		case store.BackupSucceeded:
			return got
		case store.BackupFailed:
			e.t.Fatalf("backup failed: %s", got.Error)
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.t.Fatal("backup timed out")
	return store.Backup{}
}

// restore restores b to completion.
func (e *itEnv) restore(b store.Backup) {
	e.t.Helper()
	r, err := e.m.Restore(e.ctx, b.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	for range 1500 {
		got, err := e.st.LatestRestore(e.ctx, r.ServiceID)
		if err != nil {
			e.t.Fatal(err)
		}
		switch got.Status {
		case store.RestoreSucceeded:
			return
		case store.RestoreFailed:
			e.t.Fatalf("restore failed: %s", got.Error)
		}
		time.Sleep(200 * time.Millisecond)
	}
	e.t.Fatal("restore timed out")
}

func checkMethod(t *testing.T, b store.Backup, method store.BackupMethod, ext string) {
	t.Helper()
	if b.Method != method || !strings.HasSuffix(b.File, ext+".zst.age") || !b.Encrypted || b.Size == 0 {
		t.Fatalf("backup = %+v, want method %s, an encrypted %s archive", b, method, ext)
	}
}

func TestIntegrationPostgres(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("postgres")
	const ping = `pg_isready -q -h 127.0.0.1`
	psql := func(db, sql string) string {
		return e.mustSh(s, fmt.Sprintf(`psql -X -qtA -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d %s -c %q`, db, sql))
	}
	e.waitReady(s, "postgres", ping)
	psql("app", `CREATE TABLE t (v text); INSERT INTO t VALUES ('original')`)
	psql("postgres", `CREATE DATABASE other`)
	psql("other", `CREATE TABLE o (v text); INSERT INTO o VALUES ('other')`)
	psql("postgres", `CREATE ROLE reader LOGIN PASSWORD 'pw'`)

	b := e.backUp(s)
	checkMethod(t, b, store.MethodDump, ".sql")

	psql("app", `UPDATE t SET v = 'changed'; CREATE TABLE extra (x int)`)
	psql("postgres", `DROP DATABASE other`)
	psql("postgres", `DROP ROLE reader`)
	psql("postgres", `CREATE DATABASE newer`)

	// An app connected to the database must not keep it from being dropped
	// and recreated.
	sessCtx, endSession := context.WithCancel(e.ctx)
	defer endSession()
	go e.dc.Exec(sessCtx, e.container(s), []string{"psql", "-U", "postgres", "-d", "app", "-c", "SELECT pg_sleep(60)"}, nil, io.Discard, io.Discard)
	time.Sleep(time.Second)

	e.restore(b)
	e.waitReady(s, "postgres", ping)
	if got := psql("app", `SELECT v FROM t`); got != "original" {
		t.Errorf("t.v = %q, want original", got)
	}
	if got := psql("app", `SELECT count(*) FROM pg_tables WHERE tablename = 'extra'`); got != "0" {
		t.Errorf("table created after the backup survived the restore")
	}
	if got := psql("other", `SELECT v FROM o`); got != "other" {
		t.Errorf("o.v = %q, want other", got)
	}
	if got := psql("postgres", `SELECT count(*) FROM pg_roles WHERE rolname = 'reader'`); got != "1" {
		t.Errorf("role reader not restored")
	}
	if got := psql("postgres", `SELECT count(*) FROM pg_database WHERE datname = 'newer'`); got != "0" {
		t.Errorf("database created after the backup survived the restore")
	}
	checkPreRestore(t, e, s)
}

// checkPreRestore checks that the restore took a pre-restore backup.
func checkPreRestore(t *testing.T, e *itEnv, s itService) {
	t.Helper()
	bs, err := e.st.Backups(e.ctx, s.sv.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 || bs[0].Trigger != store.BackupPreRestore || bs[0].Status != store.BackupSucceeded {
		t.Errorf("backups = %+v, want a successful pre-restore backup first", bs)
	}
}

func TestIntegrationMySQL(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("mysql")
	const ping = `MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysqladmin -h127.0.0.1 -uroot ping`
	mysql := func(sql string) string {
		return e.mustSh(s, fmt.Sprintf(`MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B app -e %q`, sql))
	}
	e.waitReady(s, "mysqld", ping)
	mysql(`CREATE TABLE t (v varchar(20)); INSERT INTO t VALUES ('original')`)

	b := e.backUp(s)
	checkMethod(t, b, store.MethodDump, ".sql")

	mysql("UPDATE t SET v = 'changed'; INSERT INTO t VALUES ('extra'); CREATE TABLE extra (x int); " +
		"CREATE DATABASE newer; CREATE TABLE newer.n (x int)")

	e.restore(b)
	e.waitReady(s, "mysqld", ping)
	if got := mysql(`SELECT group_concat(v) FROM t`); got != "original" {
		t.Errorf("t = %q, want original", got)
	}
	if got := mysql(`SELECT count(*) FROM information_schema.tables WHERE table_name IN ('extra', 'n')`); got != "0" {
		t.Errorf("%s tables created after the backup survived the restore", got)
	}
	if got := mysql(`SELECT count(*) FROM information_schema.schemata WHERE schema_name = 'newer'`); got != "0" {
		t.Errorf("database created after the backup survived the restore")
	}
	checkPreRestore(t, e, s)
}

func TestIntegrationMongo(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("mongo")
	mongosh := func(js string) string {
		return e.mustSh(s, fmt.Sprintf(`mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval '%s'`, js))
	}
	const ping = `mongosh --quiet --eval 'db.runCommand({ping: 1}).ok' | grep -q 1`
	e.waitReady(s, "mongod", ping)
	mongosh(`db.getSiblingDB("app").t.insertOne({v: "original"})`)

	b := e.backUp(s)
	checkMethod(t, b, store.MethodDump, ".archive")

	mongosh(`const t = db.getSiblingDB("app").t; t.updateOne({}, {$set: {v: "changed"}}); t.insertOne({v: "extra"});
		db.getSiblingDB("app").extra.insertOne({x: 1}); db.getSiblingDB("newer").n.insertOne({x: 1})`)

	e.restore(b)
	e.waitReady(s, "mongod", ping)
	got := mongosh(`db.getSiblingDB("app").t.find().toArray().map(d => d.v).join(",")`)
	if got != "original" {
		t.Errorf("t = %q, want original", got)
	}
	if got := mongosh(`db.getSiblingDB("app").getCollectionNames().join(",")`); got != "t" {
		t.Errorf("collections of app = %q, want only t", got)
	}
	if got := mongosh(`db.adminCommand({listDatabases: 1, nameOnly: true}).databases.map(d => d.name).filter(n => n == "newer").length`); got != "0" {
		t.Errorf("database created after the backup survived the restore")
	}
	checkPreRestore(t, e, s)
}

func TestIntegrationRedis(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("redis")
	cli := func(args string) string {
		return e.mustSh(s, `REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli `+args)
	}
	const ping = `REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli PING | grep -q PONG`
	e.waitReady(s, "redis-server", ping)
	cli(`SET k original`)

	b := e.backUp(s)
	checkMethod(t, b, store.MethodDump, ".rdb")

	cli(`SET k changed`)
	cli(`SET extra 1`)

	e.restore(b)
	e.waitReady(s, "redis-server", ping)
	if got := cli(`GET k`); got != "original" {
		t.Errorf("k = %q, want original", got)
	}
	if got := cli(`EXISTS extra`); got != "0" {
		t.Errorf("EXISTS extra = %q, want 0", got)
	}
	// The restored data must survive a restart, which loads the AOF.
	if err := e.dc.Restart(e.ctx, e.container(s), 10*time.Second); err != nil {
		t.Fatal(err)
	}
	e.waitReady(s, "redis-server", ping)
	if got := cli(`GET k`); got != "original" {
		t.Errorf("after restart k = %q, want original", got)
	}
	checkPreRestore(t, e, s)
}

func TestIntegrationAppVolume(t *testing.T) {
	e := newITEnv(t)
	s := e.startService("app", "alpine:3", nil, []string{"sleep", "infinity"}, "/srv/data")
	e.mustSh(s, `set -e
		mkdir -p /srv/data/sub/deep
		echo original > /srv/data/sub/deep/file
		echo keep > /srv/data/top
		chown 1234:5678 /srv/data/top
		chmod 640 /srv/data/top
		ln -s sub/deep/file /srv/data/link
		ln /srv/data/top /srv/data/hard`)

	b := e.backUp(s)
	checkMethod(t, b, store.MethodVolume, ".tar")

	e.mustSh(s, `echo changed > /srv/data/sub/deep/file; rm /srv/data/top; touch /srv/data/extra`)

	e.restore(b)
	got := e.mustSh(s, `cd /srv/data && cat sub/deep/file link && stat -c '%u:%g %a %h' top && readlink link && ls`)
	want := "original\noriginal\n1234:5678 640 2\nsub/deep/file\nhard\nlink\nsub\ntop"
	if got != want {
		t.Errorf("restored volume:\n%s\nwant:\n%s", got, want)
	}
	checkPreRestore(t, e, s)
}

func TestIntegrationStoppedPostgres(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("postgres")
	const ping = `pg_isready -q -h 127.0.0.1`
	psql := func(sql string) string {
		return e.mustSh(s, fmt.Sprintf(`psql -X -qtA -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d app -c %q`, sql))
	}
	e.waitReady(s, "postgres", ping)
	psql(`CREATE TABLE t (v text); INSERT INTO t VALUES ('original')`)
	if err := e.dc.Stop(e.ctx, e.container(s), 30*time.Second); err != nil {
		t.Fatal(err)
	}

	b := e.backUp(s)
	checkMethod(t, b, store.MethodVolume, ".tar")
	// The hold was released: the service runs again.
	e.waitReady(s, "postgres", ping)
	psql(`UPDATE t SET v = 'changed'`)

	e.restore(b)
	e.waitReady(s, "postgres", ping)
	if got := psql(`SELECT v FROM t`); got != "original" {
		t.Errorf("t.v = %q, want original", got)
	}
	bs, err := e.st.Backups(e.ctx, s.sv.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 || bs[0].Trigger != store.BackupPreRestore || bs[0].Method != store.MethodDump {
		t.Errorf("backups = %+v, want a pre-restore dump of the running database first", bs)
	}

	// The downloaded archive is the plain tar.zst.
	rc, err := e.m.Open(e.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	dec, err := decompress(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	present, _, err := scanVolumes(dec, []string{s.vol.MountPath})
	if err != nil || len(present) != 1 {
		t.Errorf("downloaded archive has volumes %v, %v", present, err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
}

// blockedWriter blocks every write until done is closed, like a shed that
// hangs or died while reading a dump.
type blockedWriter struct{ done chan struct{} }

func (w blockedWriter) Write(p []byte) (int, error) {
	<-w.done
	return 0, errors.New("closed")
}

// TestIntegrationMongoWriteLock checks the write lock of mongo dumps, as an
// app on the network sees it: writes wait while a dump runs, and resume
// soon after the dump stalls because shed stopped reading it, or shed drops
// the dump, whether or not it could end it. Execs into the database's
// container hang while a dump's output is not read, so a client container
// does the writes.
func TestIntegrationMongoWriteLock(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("mongo")
	e.waitReady(s, "mongod", `mongosh --quiet --eval 'db.runCommand({ping: 1}).ok' | grep -q 1`)
	const auth = `-u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin`
	// Enough data that the dump fills the pipes and blocks.
	e.mustSh(s, `mongosh --quiet `+auth+` --eval 'const t = db.getSiblingDB("app").big; for (let i = 0; i < 40; i++) {
		t.insertMany(Array.from({length: 100}, () => ({s: Array.from({length: 120}, () => Math.random().toString(36)).join("")})));
	}'`)
	user := e.mustSh(s, `printf %s "$MONGO_INITDB_ROOT_USERNAME"`)
	pw := e.mustSh(s, `printf %s "$MONGO_INITDB_ROOT_PASSWORD"`)
	ctr, err := e.dc.Inspect(e.ctx, e.container(s))
	if err != nil {
		t.Fatal(err)
	}
	client, err := e.dc.Run(e.ctx, docker.RunSpec{
		Name: "shed-it-client-" + s.sv.ID, Image: "mongo:8", Cmd: []string{"sleep", "infinity"},
		Labels: map[string]string{"shed.it": s.sv.ID}, // Removed with the service.
	})
	if err != nil {
		t.Fatal(err)
	}
	canWrite := func() bool {
		cmd := []string{"timeout", "3", "mongosh", "--quiet", "--host", ctr.IPs["bridge"], "-u", user, "-p", pw,
			"--authenticationDatabase", "admin", "--eval", `db.getSiblingDB("app").w.insertOne({x: 1})`}
		return e.dc.Exec(e.ctx, client, cmd, nil, io.Discard, io.Discard) == nil
	}
	waitWrites := func(want bool, what string) {
		t.Helper()
		start := time.Now()
		for canWrite() != want {
			if time.Since(start) > 45*time.Second {
				t.Fatalf("writes did not %s", what)
			}
		}
		t.Logf("writes %s after %v", what, time.Since(start).Round(time.Second))
	}

	for _, tt := range []struct {
		name  string
		stall int
		// drop cancels the exec, as a canceled job or a crash of shed
		// does; end then removes the run file, as shed does after a
		// canceled or failed dump.
		drop, end bool
	}{
		{name: "shed stops reading", stall: 3},
		{name: "shed cancels the dump", stall: 600, drop: true, end: true},
		{name: "shed dies", stall: 5, drop: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := "/tmp/shed-backup-" + store.NewID()
			w := blockedWriter{done: make(chan struct{})}
			ctx, cancel := context.WithCancel(e.ctx)
			var wg sync.WaitGroup
			wg.Go(func() {
				e.dc.Exec(ctx, e.container(s), []string{"sh", "-c", mongoDump(dir, tt.stall)}, nil, w, io.Discard)
			})
			unblock := sync.OnceFunc(func() { cancel(); close(w.done); wg.Wait() })
			defer unblock()

			waitWrites(false, "stop while the dump runs")
			if tt.drop {
				unblock()
			}
			if tt.end {
				e.mustSh(s, "rm -f '"+dir+"/run'")
			}
			waitWrites(true, "resume")
			unblock()

			unlock := `try { db.adminCommand({fsyncUnlock: 1}); print("was locked") } catch (e) { print(e.message) }`
			if out, _ := e.sh(s, `mongosh --quiet `+auth+` --eval '`+unlock+`'`); !strings.Contains(out, "not locked") {
				t.Errorf("fsyncUnlock after the dump: %q, want not locked", out)
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := e.sh(s, "test -e '"+dir+"'"); err != nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s left behind", dir)
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}

// TestIntegrationMongoConsistent checks that a mongo dump taken while an app
// writes to two collections in step sees both at the same moment.
func TestIntegrationMongoConsistent(t *testing.T) {
	e := newITEnv(t)
	s := e.catalogService("mongo")
	const auth = `-u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin`
	e.waitReady(s, "mongod", `mongosh --quiet --eval 'db.runCommand({ping: 1}).ok' | grep -q 1`)
	pad := `Array.from({length: 60}, () => Math.random().toString(36)).join("")`
	e.mustSh(s, fmt.Sprintf(`mongosh --quiet %s --eval 'const d = db.getSiblingDB("app");
		for (let i = 0; i < 20; i++) { d.a.insertMany(Array.from({length: 100}, () => ({p: %s}))); d.b.insertMany(Array.from({length: 100}, () => ({p: %s}))); }'`,
		auth, pad, pad))

	// The writer adds one document to a, then one to b, until stopped.
	wctx, stop := context.WithCancel(e.ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		e.dc.Exec(wctx, e.container(s), []string{"sh", "-c", fmt.Sprintf(`mongosh --quiet %s --eval 'const d = db.getSiblingDB("app");
			while (!require("fs").existsSync("/tmp/stop-writer")) { const p = %s; d.a.insertOne({p}); d.b.insertOne({p}); }'`, auth, pad)},
			nil, io.Discard, io.Discard)
	})
	time.Sleep(2 * time.Second)
	b := e.backUp(s)
	e.mustSh(s, "touch /tmp/stop-writer")
	wg.Wait()
	stop()

	rc, err := e.m.Open(e.ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	archive, err := decompress(rc)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var errOut bytes.Buffer
	script := `mongorestore --quiet ` + auth + ` --archive --nsInclude 'app.*' --nsFrom 'app.*' --nsTo 'check.*'`
	if err := e.dc.Exec(e.ctx, e.container(s), []string{"sh", "-c", script}, archive, io.Discard, &errOut); err != nil {
		t.Fatalf("%v: %s", err, errOut.String())
	}
	got := e.mustSh(s, `mongosh --quiet `+auth+` --eval 'const d = db.getSiblingDB("check"); print(d.a.countDocuments() + " " + d.b.countDocuments())'`)
	var na, nb int
	if _, err := fmt.Sscan(got, &na, &nb); err != nil {
		t.Fatalf("counts %q: %v", got, err)
	}
	// The lock can fall between the two inserts of one step.
	if na != nb && na != nb+1 {
		t.Errorf("dump has %d documents in a and %d in b, want the same or one more in a", na, nb)
	}
	t.Logf("dump has %d documents in a and %d in b", na, nb)
}
