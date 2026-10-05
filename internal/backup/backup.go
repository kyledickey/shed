// Package backup backs up and restores the data of shed's services and
// shed's own database.
//
// A [Manager] writes each backup as one archive file: a database dump, a tar
// of the service's volumes, or a snapshot of shed.db, compressed with zstd
// and optionally encrypted with age. Archives are kept under a local
// directory and optionally uploaded to S3-compatible storage. The Manager
// runs scheduled backups from cron policies, prunes old archives, and
// restores service backups. One backup or restore runs at a time.
//
// The store, Docker, the deployer, and S3 are reached through small
// interfaces so they can be replaced, for example by fakes in tests.
package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// Store persists backups, restores, policies, and settings. *store.Store
// implements it.
type Store interface {
	Service(ctx context.Context, id string) (store.Service, error)
	SetServiceStopped(ctx context.Context, id string, stopped bool) error
	Volumes(ctx context.Context, serviceID string) ([]store.Volume, error)
	ActiveDeployment(ctx context.Context, serviceID string) (store.Deployment, error)
	ServicesWithVolumes(ctx context.Context) ([]store.Service, error)
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	AddSetting(ctx context.Context, key, value string) (bool, error)
	PutBackupDestination(ctx context.Context, d store.BackupDestination) (store.BackupDestination, error)
	BackupDestination(ctx context.Context, id string) (store.BackupDestination, error)
	BackupPolicy(ctx context.Context, serviceID string) (store.BackupPolicy, error)
	PutBackupPolicy(ctx context.Context, p store.BackupPolicy) error
	CreateBackup(ctx context.Context, b store.Backup) (store.Backup, error)
	UpdateBackup(ctx context.Context, b store.Backup) error
	Backup(ctx context.Context, id string) (store.Backup, error)
	Backups(ctx context.Context, serviceID string, limit int) ([]store.Backup, error)
	DeleteBackup(ctx context.Context, id string) error
	DeleteFailedBackupsBefore(ctx context.Context, t time.Time) (int64, error)
	UploadingBackups(ctx context.Context) ([]store.Backup, error)
	FailInterruptedBackups(ctx context.Context, msg string) (int64, error)
	CreateRestore(ctx context.Context, r store.Restore) (store.Restore, error)
	UpdateRestore(ctx context.Context, r store.Restore) error
	FailInterruptedRestores(ctx context.Context, msg string) (int64, error)
	CreateRestoreFence(ctx context.Context, f store.RestoreFence) (store.RestoreFence, error)
	SetRestorePhase(ctx context.Context, serviceID string, phase store.RestorePhase) error
	LiftRestoreFence(ctx context.Context, serviceID string) error
	DeleteRestoreFence(ctx context.Context, serviceID string) error
	RestoreFence(ctx context.Context, serviceID string) (store.RestoreFence, error)
	RestoreFences(ctx context.Context) ([]store.RestoreFence, error)
	Snapshot(ctx context.Context, path string) error
}

// Docker runs dumps in containers, loads dumps into isolated copies of
// them, and reads and writes volumes through helper containers.
// *docker.Client implements it.
type Docker interface {
	Inspect(ctx context.Context, id string) (docker.Container, error)
	Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error
	Create(ctx context.Context, spec docker.RunSpec) (string, error)
	CreateIsolated(ctx context.Context, id, name string, labels map[string]string) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Remove(ctx context.Context, id string) error
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	CopyFrom(ctx context.Context, id, path string) (io.ReadCloser, error)
	CopyTo(ctx context.Context, id, dir string, r io.Reader) error
	EnsureVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
}

// Services gives a backup or restore exclusive control of a service.
type Services interface {
	// Hold cancels the service's deployments in progress and blocks
	// deploying, starting, stopping, restarting, and deleting it until the
	// hold is released.
	Hold(ctx context.Context, serviceID string) (Held, error)
}

// Held is a service under a hold.
type Held interface {
	// Active returns the active deployment, or store.ErrNotFound.
	Active(ctx context.Context) (store.Deployment, error)
	// Running reports whether the active deployment's container is running.
	Running(ctx context.Context) (bool, error)
	// StopAndRemove stops and removes the active container. The deployment
	// stays active.
	StopAndRemove(ctx context.Context) error
	// Release starts the active deployment again unless the user stopped
	// the service, and always lifts the hold.
	Release(ctx context.Context) error
}

// Remote stores archives in S3-compatible storage.
type Remote interface {
	// Put uploads r as key. A size of -1 means unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Get opens key for reading.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes key. A missing object is not an error.
	Delete(ctx context.Context, key string) error
	// Check verifies access by writing, reading, and deleting key.
	Check(ctx context.Context, key string) error
}

// Config configures a Manager.
type Config struct {
	Store    Store
	Docker   Docker
	Services Services
	// NewRemote returns a client for an S3 destination. It should validate
	// the configuration without contacting the server.
	NewRemote func(S3Config) (Remote, error)
	// Dir holds the local archives, in a subdirectory per service and
	// "system" for shed.db.
	Dir string
	Log *slog.Logger
	// Now returns the current time. Nil means time.Now.
	Now func() time.Time
}

var (
	// ErrInvalid is wrapped by errors about invalid input. Their messages
	// are meant to be shown to the user.
	ErrInvalid = errors.New("backup: invalid request")
	// ErrBusy is returned when a conflicting backup or restore is queued or
	// running, or the service is paused.
	ErrBusy = errors.New("backup: a backup or restore is already in progress")
	// ErrNoVolumes is returned when backing up a service without volumes.
	ErrNoVolumes = errors.New("backup: service has no volumes")
	// ErrFenced is returned when restoring into a service that an earlier
	// failed restore left fenced. The fence must be cleared first.
	ErrFenced = errors.New("backup: service is fenced by a failed restore")
	// ErrStopped is returned for requests after Run has returned.
	ErrStopped = errors.New("backup: manager is stopped")
)

// invalidError is an ErrInvalid with a message for the user.
type invalidError struct{ msg string }

func (e invalidError) Error() string        { return e.msg }
func (e invalidError) Is(target error) bool { return target == ErrInvalid }

func invalidf(format string, args ...any) error {
	return invalidError{fmt.Sprintf(format, args...)}
}

// errNotDeployed is a backup of a service without an active deployment.
var errNotDeployed = invalidError{"service has not been deployed yet"}

// Causes of canceled jobs.
var (
	errForgotten = errors.New("service deleted")
	errPaused    = errors.New("canceled: service is being deleted")
	errShutdown  = errors.New("interrupted by shutdown")
)

// errRestart is recorded for jobs that a restart of shed interrupted.
const errRestart = "interrupted by restart"

// failedRetention is how long failed backups are kept.
const failedRetention = 30 * 24 * time.Hour

// job is a queued or running backup or restore.
type job struct {
	serviceID string // "" = shed.db
	backup    store.Backup
	restore   *store.Restore // set for restores
	done      chan struct{}

	// ctx and cancel are set when the job starts.
	ctx    context.Context
	cancel context.CancelCauseFunc
}

// Manager runs backups and restores. Its methods are safe for concurrent use.
type Manager struct {
	store     Store
	docker    Docker
	services  Services
	newRemote func(S3Config) (Remote, error)
	dir       string
	log       *slog.Logger
	now       func() time.Time
	// readyTimeout is how long the isolated database that a dump is
	// loaded into gets to accept connections.
	readyTimeout time.Duration
	// mongoStall is how long a mongo dump may write nothing before it is
	// ended and its write lock released; see mongoDump.
	mongoStall time.Duration

	mu      sync.Mutex
	queue   []*job
	running *job
	stopped bool
	wake    chan struct{}
	next    map[string]scheduled // by target
	paused  map[string]int       // pauses by service ID
	// destroying holds the IDs of backups whose archives Delete or prune
	// is removing. Restore refuses them.
	destroying map[string]bool
}

// New returns a Manager. Call Recover and then Run.
func New(cfg Config) *Manager {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	newRemote := cfg.NewRemote
	if newRemote == nil {
		newRemote = func(S3Config) (Remote, error) { return nil, errors.New("S3 is not available") }
	}
	return &Manager{
		store:        cfg.Store,
		docker:       cfg.Docker,
		services:     cfg.Services,
		newRemote:    newRemote,
		dir:          cfg.Dir,
		log:          log,
		now:          func() time.Time { return now().UTC().Truncate(time.Millisecond) },
		readyTimeout: 5 * time.Minute,
		mongoStall:   time.Minute,
		wake:         make(chan struct{}, 1),
		next:         make(map[string]scheduled),
		paused:       make(map[string]int),
		destroying:   make(map[string]bool),
	}
}

// targetDir returns the local directory of a target's archives.
func (m *Manager) targetDir(serviceID string) string {
	if serviceID == "" {
		return filepath.Join(m.dir, "system")
	}
	return filepath.Join(m.dir, serviceID)
}

// localPath returns the path of a backup's local archive.
func (m *Manager) localPath(b store.Backup) string {
	return filepath.Join(m.targetDir(b.ServiceID), b.File)
}

// checkService returns an error wrapping store.ErrNotFound for an unknown
// service. The empty ID, shed.db, always exists.
func (m *Manager) checkService(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return nil
	}
	if _, err := m.store.Service(ctx, serviceID); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// Recover cleans up after an unclean stop: it marks queued and running
// backups and running restores failed, uploading backups succeeded (see
// recoverUploads), removes partial files and leftover helper containers, and
// finishes the restores that were interrupted while their service was
// fenced. Those services get their previous data back if their volumes may
// hold partial data; a service whose data cannot be put back stays stopped
// and fenced until the fence is cleared, and the error is logged. Call Recover before anything starts
// services, in particular before the deployer reconciles, and before Run.
func (m *Manager) Recover(ctx context.Context) error {
	if err := m.recoverUploads(ctx); err != nil {
		return fmt.Errorf("backup: recover: %w", err)
	}
	if _, err := m.store.FailInterruptedBackups(ctx, errRestart); err != nil {
		return fmt.Errorf("backup: recover: %w", err)
	}
	if _, err := m.store.FailInterruptedRestores(ctx, errRestart); err != nil {
		return fmt.Errorf("backup: recover: %w", err)
	}
	err := filepath.WalkDir(m.dir, func(p string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == m.dir {
			return fs.SkipAll
		}
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, partialSuffix) {
			return os.Remove(p)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("backup: recover: remove partial files: %w", err)
	}
	helpers, err := m.docker.List(ctx, map[string]string{helperLabel: ""})
	if err != nil {
		return fmt.Errorf("backup: recover: %w", err)
	}
	for _, c := range helpers {
		m.removeHelper(ctx, c.ID)
	}
	fences, err := m.store.RestoreFences(ctx)
	if err != nil {
		return fmt.Errorf("backup: recover: %w", err)
	}
	for _, f := range fences {
		if err := m.recoverFence(ctx, f); err != nil {
			m.log.Error("restore: recover an interrupted restore", "service", f.ServiceID, "restore", f.RestoreID, "err", err)
		}
	}
	return nil
}

// Run runs queued jobs one at a time, in order, and starts scheduled backups
// every minute, until ctx is done. It then cancels the running job, waits for
// it to stop, and marks the queued ones failed.
func (m *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { m.work(ctx) })

	timer := time.NewTimer(untilNextMinute(time.Now()))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			m.shutdown(context.WithoutCancel(ctx))
			return
		case <-timer.C:
			m.tick(ctx, m.now())
			timer.Reset(untilNextMinute(time.Now()))
		}
	}
}

// untilNextMinute returns the time from t to just after the next minute
// starts.
func untilNextMinute(t time.Time) time.Duration {
	return t.Truncate(time.Minute).Add(time.Minute + time.Second).Sub(t)
}

// work runs jobs until ctx is done.
func (m *Manager) work(ctx context.Context) {
	for {
		j := m.take(ctx)
		if j == nil {
			return
		}
		m.execute(ctx, j)
	}
}

// take waits for the next job and marks it running. It returns nil once ctx
// is done.
func (m *Manager) take(ctx context.Context) *job {
	for {
		m.mu.Lock()
		if ctx.Err() == nil && len(m.queue) > 0 {
			j := m.queue[0]
			m.queue = m.queue[1:]
			m.running = j
			j.ctx, j.cancel = context.WithCancelCause(ctx)
			m.mu.Unlock()
			return j
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil
		case <-m.wake:
		}
	}
}

// execute runs j and records its outcome.
func (m *Manager) execute(ctx context.Context, j *job) {
	defer func() {
		j.cancel(nil)
		m.mu.Lock()
		m.running = nil
		m.mu.Unlock()
		close(j.done)
	}()
	if j.restore != nil {
		m.runRestore(j.ctx, *j.restore, j.backup)
	} else {
		m.runBackup(j.ctx, j.backup)
	}
	if _, err := m.store.DeleteFailedBackupsBefore(context.WithoutCancel(ctx), m.now().Add(-failedRetention)); err != nil {
		m.log.Error("backup: delete old failed backups", "err", err)
	}
}

// shutdown marks the jobs still queued failed and rejects new ones.
func (m *Manager) shutdown(ctx context.Context) {
	m.mu.Lock()
	queued := m.queue
	m.queue = nil
	m.stopped = true
	m.mu.Unlock()
	for _, j := range queued {
		m.failQueued(ctx, j, errShutdown.Error())
		close(j.done)
	}
}

// failQueued records that a job never ran.
func (m *Manager) failQueued(ctx context.Context, j *job, msg string) {
	now := m.now()
	if j.restore != nil {
		r := *j.restore
		r.Status, r.Error, r.FinishedAt = store.RestoreFailed, msg, &now
		if err := m.store.UpdateRestore(ctx, r); err != nil && !errors.Is(err, store.ErrNotFound) {
			m.log.Error("backup: record canceled restore", "restore", r.ID, "err", err)
		}
		return
	}
	b := j.backup
	b.Status, b.Error, b.FinishedAt = store.BackupFailed, msg, &now
	if err := m.store.UpdateBackup(ctx, b); err != nil && !errors.Is(err, store.ErrNotFound) {
		m.log.Error("backup: record canceled backup", "backup", b.ID, "err", err)
	}
}

// busy reports whether a job of the target is queued or running. If
// restoresOnly is set, only restores count. The caller holds m.mu.
func (m *Manager) busy(serviceID string, restoresOnly bool) bool {
	for _, j := range m.jobs() {
		if j.serviceID == serviceID && (!restoresOnly || j.restore != nil) {
			return true
		}
	}
	return false
}

// jobs returns the running job, if any, and the queued ones. The caller
// holds m.mu.
func (m *Manager) jobs() []*job {
	if m.running == nil {
		return m.queue
	}
	return append([]*job{m.running}, m.queue...)
}

// inUse reports whether a queued or running job uses the backup id, as the
// backup it writes or the backup it restores, or its archives are being
// removed. The caller holds m.mu.
func (m *Manager) inUse(id string) bool {
	return m.destroying[id] || slices.ContainsFunc(m.jobs(), func(j *job) bool { return j.backup.ID == id })
}

// claim marks the backups ids that are not in use as being destroyed and
// returns them. Call the returned function once their archives are removed
// and their records updated.
func (m *Manager) claim(ids []string) (claimed map[string]bool, release func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	claimed = make(map[string]bool)
	for _, id := range ids {
		if !m.inUse(id) {
			claimed[id], m.destroying[id] = true, true
		}
	}
	return claimed, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for id := range claimed {
			delete(m.destroying, id)
		}
	}
}

// push queues j. The caller holds m.mu.
func (m *Manager) push(j *job) {
	j.done = make(chan struct{})
	m.queue = append(m.queue, j)
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// methodNow chooses how to back up a service in its current state: a dump
// of a running database, or otherwise an archive of its volumes. It returns
// errNotDeployed if the service has no active deployment.
func (m *Manager) methodNow(ctx context.Context, sv store.Service) (store.BackupMethod, error) {
	d, err := m.store.ActiveDeployment(ctx, sv.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "", errNotDeployed
	}
	if err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	running, err := m.containerRunning(ctx, d)
	if err != nil {
		return "", err
	}
	return chooseMethod(sv.Kind, running), nil
}

// chooseMethod returns the backup method for a service of kind whose
// container is running or not.
func chooseMethod(kind string, running bool) store.BackupMethod {
	if isDatabase(kind) && running {
		return store.MethodDump
	}
	return store.MethodVolume
}

// containerRunning reports whether the container of deployment d runs.
func (m *Manager) containerRunning(ctx context.Context, d store.Deployment) (bool, error) {
	if d.ContainerID == "" {
		return false, nil
	}
	c, err := m.docker.Inspect(ctx, d.ContainerID)
	if docker.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("backup: %w", err)
	}
	return c.Running, nil
}

// newBackup returns a queued backup row of a target, checking that it can be
// backed up. It does not store it.
func (m *Manager) newBackup(ctx context.Context, serviceID string, trigger store.BackupTrigger) (store.Backup, error) {
	b := store.Backup{
		ID:        store.NewID(),
		ServiceID: serviceID,
		Trigger:   trigger,
		Method:    store.MethodSQLite,
		Status:    store.BackupQueued,
		CreatedAt: m.now(),
	}
	if serviceID == "" {
		return b, nil
	}
	sv, err := m.store.Service(ctx, serviceID)
	if err != nil {
		return store.Backup{}, fmt.Errorf("backup: %w", err)
	}
	vols, err := m.store.Volumes(ctx, serviceID)
	if err != nil {
		return store.Backup{}, fmt.Errorf("backup: %w", err)
	}
	if len(vols) == 0 {
		return store.Backup{}, ErrNoVolumes
	}
	if b.Method, err = m.methodNow(ctx, sv); err != nil {
		return store.Backup{}, err
	}
	return b, nil
}

// enqueueBackup queues a backup of a target.
func (m *Manager) enqueueBackup(ctx context.Context, serviceID string, trigger store.BackupTrigger) (store.Backup, error) {
	b, err := m.newBackup(ctx, serviceID, trigger)
	if err != nil {
		return store.Backup{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return store.Backup{}, ErrStopped
	}
	if m.paused[serviceID] > 0 || m.busy(serviceID, false) {
		return store.Backup{}, ErrBusy
	}
	if b, err = m.store.CreateBackup(ctx, b); err != nil {
		return store.Backup{}, fmt.Errorf("backup: %w", err)
	}
	m.push(&job{serviceID: serviceID, backup: b})
	return b, nil
}

// BackUp queues a manual backup of a service, or of shed.db for an empty
// serviceID. It returns ErrBusy if the service already has a backup or
// restore queued or running, or is paused, ErrNoVolumes if it has no volumes,
// and an error wrapping ErrInvalid if it was never deployed.
func (m *Manager) BackUp(ctx context.Context, serviceID string) (store.Backup, error) {
	return m.enqueueBackup(ctx, serviceID, store.BackupManual)
}

// Backups returns the backups of a service, or of shed.db for an empty
// serviceID, newest first. A limit of zero or less returns all of them.
func (m *Manager) Backups(ctx context.Context, serviceID string, limit int) ([]store.Backup, error) {
	if err := m.checkService(ctx, serviceID); err != nil {
		return nil, err
	}
	bs, err := m.store.Backups(ctx, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	return bs, nil
}

// Restore queues a restore of a service backup into its service. It returns
// an error wrapping ErrInvalid for backups of shed.db, backups that did not
// succeed, and backups whose archive is gone, ErrBusy if a restore of the
// service is already queued or running or the service is paused, and
// ErrFenced if a failed restore left the service fenced.
func (m *Manager) Restore(ctx context.Context, backupID string) (store.Restore, error) {
	// The backup is read and its restore queued under m.mu, so that Delete
	// and prune either see the queued restore and keep the archive, or
	// have removed it before the backup is read.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return store.Restore{}, ErrStopped
	}
	if m.destroying[backupID] {
		return store.Restore{}, ErrBusy
	}
	b, err := m.store.Backup(ctx, backupID)
	if err != nil {
		return store.Restore{}, fmt.Errorf("backup: %w", err)
	}
	switch {
	case b.ServiceID == "":
		return store.Restore{}, invalidf("backups of shed's own database are restored by hand")
	case b.Status != store.BackupSucceeded:
		return store.Restore{}, invalidf("only successful backups can be restored")
	case !b.Local && b.RemoteKey == "":
		return store.Restore{}, invalidf("the backup's archive no longer exists")
	}
	if m.paused[b.ServiceID] > 0 || m.busy(b.ServiceID, true) {
		return store.Restore{}, ErrBusy
	}
	// A new restore would copy the fenced, possibly partial data over the
	// pre-restore volumes that may hold the previous data.
	switch _, err := m.store.RestoreFence(ctx, b.ServiceID); {
	case err == nil:
		return store.Restore{}, ErrFenced
	case !errors.Is(err, store.ErrNotFound):
		return store.Restore{}, fmt.Errorf("backup: %w", err)
	}
	r, err := m.store.CreateRestore(ctx, store.Restore{
		ServiceID: b.ServiceID,
		BackupID:  b.ID,
		Status:    store.RestoreRunning,
		CreatedAt: m.now(),
	})
	if err != nil {
		return store.Restore{}, fmt.Errorf("backup: %w", err)
	}
	m.push(&job{serviceID: b.ServiceID, backup: b, restore: &r})
	return r, nil
}

// Delete deletes a backup: its local archive, its S3 object, and its record.
// It returns ErrBusy while the backup is queued or running, a queued or
// running restore uses it, or it is already being deleted.
func (m *Manager) Delete(ctx context.Context, backupID string) error {
	claimed, release := m.claim([]string{backupID})
	defer release()
	if !claimed[backupID] {
		return ErrBusy
	}
	b, err := m.store.Backup(ctx, backupID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	switch b.Status {
	case store.BackupQueued, store.BackupRunning, store.BackupUploading:
		return ErrBusy
	}
	if b.Local {
		if err := os.Remove(m.localPath(b)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("backup: delete %s: %w", b.ID, err)
		}
	}
	if b.RemoteKey != "" {
		r, err := m.remoteOf(ctx, b)
		switch {
		case errors.Is(err, errNoDestination):
			m.log.Warn("backup: the S3 destination is unknown; keeping the object of a deleted backup", "backup", b.ID, "key", b.RemoteKey)
		case err != nil:
			return err
		default:
			if err := r.Delete(ctx, b.RemoteKey); err != nil {
				return fmt.Errorf("backup: delete %s: %w", b.ID, err)
			}
		}
	}
	if err := m.store.DeleteBackup(ctx, b.ID); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// Open returns the decrypted archive of a successful backup, still
// compressed with zstd. It reads the local file if there is one and
// otherwise downloads the S3 object.
func (m *Manager) Open(ctx context.Context, backupID string) (io.ReadCloser, error) {
	b, err := m.store.Backup(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	if b.Status != store.BackupSucceeded {
		return nil, invalidf("only successful backups can be downloaded")
	}
	rc, err := m.openArchive(ctx, b)
	if err != nil {
		return nil, err
	}
	if !b.Encrypted {
		return rc, nil
	}
	id, err := m.identity(ctx)
	if err != nil {
		rc.Close()
		return nil, err
	}
	dr, err := decrypt(rc, id)
	if err != nil {
		rc.Close()
		return nil, fmt.Errorf("backup: %s: %w", b.ID, err)
	}
	return readCloser{dr, rc}, nil
}

// readCloser reads from one reader and closes another.
type readCloser struct {
	io.Reader
	io.Closer
}

// openArchive opens the stored archive of b: the local file, or else the S3
// object.
func (m *Manager) openArchive(ctx context.Context, b store.Backup) (io.ReadCloser, error) {
	if b.Local {
		f, err := os.Open(m.localPath(b))
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || b.RemoteKey == "" {
			return nil, fmt.Errorf("backup: open %s: %w", b.ID, err)
		}
	}
	if b.RemoteKey == "" {
		return nil, invalidf("the backup's archive no longer exists")
	}
	r, err := m.remoteOf(ctx, b)
	if err != nil {
		return nil, err
	}
	rc, err := r.Get(ctx, b.RemoteKey)
	if err != nil {
		return nil, fmt.Errorf("backup: download %s: %w", b.ID, err)
	}
	return rc, nil
}

// DownloadName returns the file name to download a backup as, without the
// .age suffix since downloads are decrypted, for example
// "postgres-20261004-030000.sql.zst". serviceName is empty for shed.db.
func DownloadName(b store.Backup, serviceName string) string {
	if serviceName == "" {
		serviceName = "shed"
	}
	ext := strings.TrimSuffix(b.File, ".age")
	if i := strings.IndexByte(ext, '.'); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}
	return serviceName + "-" + b.CreatedAt.UTC().Format("20060102-150405") + ext
}

// cancelJobs removes the queued jobs of a service and cancels its running
// job with cause. It returns the queued jobs, which the caller must fail and
// close, and the running job's done channel, or nil. The caller holds m.mu.
func (m *Manager) cancelJobs(serviceID string, cause error) (queued []*job, done chan struct{}) {
	m.queue = slices.DeleteFunc(m.queue, func(j *job) bool {
		if j.serviceID == serviceID {
			queued = append(queued, j)
			return true
		}
		return false
	})
	if j := m.running; j != nil && j.serviceID == serviceID {
		j.cancel(cause)
		done = j.done
	}
	return queued, done
}

// finishCanceled records the queued jobs returned by cancelJobs as failed
// and waits for the running one, if any, to stop.
func (m *Manager) finishCanceled(ctx context.Context, queued []*job, done chan struct{}, cause error) error {
	for _, j := range queued {
		m.failQueued(ctx, j, cause.Error())
		close(j.done)
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PauseService cancels the queued and running backups of a service, waits
// until the running one has stopped and removed its helper container, and
// rejects new jobs for the service until resume is called: BackUp and Restore
// return ErrBusy, and scheduled backups are skipped. Call it before deleting
// the service, whose volumes a helper container would keep in use. If a
// restore of the service is running, PauseService returns ErrBusy and cancels
// nothing, since stopping a restore halfway would leave the volumes partly
// restored. Queued restores are canceled. If ctx ends while waiting, the
// service is not paused and the error wraps the context's. resume is safe to
// call more than once.
func (m *Manager) PauseService(ctx context.Context, serviceID string) (resume func(), err error) {
	if serviceID == "" {
		return nil, fmt.Errorf("backup: pause service: empty service ID")
	}
	m.mu.Lock()
	if j := m.running; j != nil && j.serviceID == serviceID && j.restore != nil {
		m.mu.Unlock()
		return nil, ErrBusy
	}
	m.paused[serviceID]++
	queued, done := m.cancelJobs(serviceID, errPaused)
	m.mu.Unlock()

	var once sync.Once
	resume = func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.paused[serviceID]--; m.paused[serviceID] <= 0 {
				delete(m.paused, serviceID)
			}
		})
	}
	if err := m.finishCanceled(ctx, queued, done, errPaused); err != nil {
		resume()
		return nil, fmt.Errorf("backup: pause service %s: %w", serviceID, err)
	}
	return resume, nil
}

// ForgetService cancels the queued and running jobs of a deleted service,
// waits for the running one to stop, and removes its local archives. Call it
// after the service is deleted: its S3 objects are kept.
func (m *Manager) ForgetService(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return fmt.Errorf("backup: forget service: empty service ID")
	}
	m.mu.Lock()
	queued, done := m.cancelJobs(serviceID, errForgotten)
	delete(m.next, serviceID)
	m.mu.Unlock()

	if err := m.finishCanceled(ctx, queued, done, errForgotten); err != nil {
		return fmt.Errorf("backup: forget service %s: %w", serviceID, err)
	}
	if err := os.RemoveAll(m.targetDir(serviceID)); err != nil {
		return fmt.Errorf("backup: forget service %s: %w", serviceID, err)
	}
	return nil
}

// recoverUploads settles the backups whose upload a restart interrupted. One
// whose local file exists stays local and records the upload as interrupted,
// forgetting the intended remote location because the object may be missing
// or partial. One without a local file is remote-only if its recorded object
// can be read, and failed otherwise. A succeeded backup is never left
// without a local file or a remote object.
func (m *Manager) recoverUploads(ctx context.Context) error {
	bs, err := m.store.UploadingBackups(ctx)
	if err != nil {
		return err
	}
	for _, b := range bs {
		finished := m.now()
		b.FinishedAt = &finished
		_, statErr := os.Stat(m.localPath(b))
		switch {
		case statErr == nil:
			b.Status, b.Local = store.BackupSucceeded, true
			b.RemoteKey, b.DestinationID, b.RemoteError = "", "", errRestart
		case m.remoteHas(ctx, b):
			b.Status, b.Local, b.RemoteError = store.BackupSucceeded, false, ""
		default:
			b.Status, b.Local, b.Error = store.BackupFailed, false, errRestart
			b.RemoteKey, b.DestinationID = "", ""
		}
		if err := m.store.UpdateBackup(ctx, b); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return nil
}

// remoteHas reports whether the object that b records exists.
func (m *Manager) remoteHas(ctx context.Context, b store.Backup) bool {
	if b.RemoteKey == "" || b.DestinationID == "" {
		return false
	}
	r, err := m.remoteOf(ctx, b)
	if err != nil {
		m.log.Error("backup: recover: open destination", "backup", b.ID, "err", err)
		return false
	}
	rc, err := r.Get(ctx, b.RemoteKey)
	if err != nil {
		return false
	}
	rc.Close()
	return true
}
