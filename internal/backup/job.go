package backup

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"filippo.io/age"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

const (
	// partialSuffix marks files being written. Recover removes them.
	partialSuffix = ".partial"
	// helperLabel labels helper containers with the backup or restore ID.
	helperLabel = "shed.backup"
)

// volumeName returns the Docker volume name of a volume.
func volumeName(v store.Volume) string { return "shed-vol-" + v.ID }

// mountPaths returns the mount paths of vols.
func mountPaths(vols []store.Volume) []string {
	mps := make([]string, len(vols))
	for i, v := range vols {
		mps[i] = v.MountPath
	}
	return mps
}

// failure returns the message to record for a job that failed with err.
func failure(ctx context.Context, err error) string {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause.Error()
	}
	if ctx.Err() != nil {
		return errShutdown.Error()
	}
	return err.Error()
}

// runBackup runs the backup b, which is stored as queued, and records the
// outcome. It returns the error the backup failed with.
func (m *Manager) runBackup(ctx context.Context, b store.Backup) error {
	started := m.now()
	b.Status, b.StartedAt = store.BackupRunning, &started
	err := m.store.UpdateBackup(ctx, b)
	var p PolicyInput
	if err == nil {
		p, err = m.policy(ctx, b.ServiceID)
	}
	if err == nil {
		err = m.produce(ctx, &b, p.Compression)
	}
	finished := m.now()
	b.FinishedAt = &finished
	if err != nil {
		b.Status, b.Error = store.BackupFailed, failure(ctx, err)
		m.log.Error("backup failed", "backup", b.ID, "service", b.ServiceID, "err", b.Error)
		if err := m.store.UpdateBackup(context.WithoutCancel(ctx), b); err != nil && !errors.Is(err, store.ErrNotFound) {
			m.log.Error("backup: record failure", "backup", b.ID, "err", err)
		}
		return errors.New(b.Error)
	}
	b.Status, b.Local = store.BackupSucceeded, true
	var r Remote
	var dest *store.BackupDestination
	if p.Upload {
		if r, dest, err = m.remote(ctx); err != nil {
			b.RemoteError = err.Error()
			m.log.Error("backup: upload failed", "backup", b.ID, "err", err)
		} else if r != nil {
			// The backup stays in progress until the upload's outcome is
			// recorded.
			b.Status, b.FinishedAt = store.BackupUploading, nil
		}
	}
	if err := m.store.UpdateBackup(context.WithoutCancel(ctx), b); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	m.log.Info("backup written", "backup", b.ID, "service", b.ServiceID, "method", b.Method, "size", b.Size)

	if r != nil {
		if err := m.upload(ctx, b, p, r, *dest); err != nil {
			return err
		}
	}
	if b.Trigger == store.BackupSchedule {
		if err := m.prune(ctx, b.ServiceID, p); err != nil {
			m.log.Error("backup: prune", "service", b.ServiceID, "err", err)
		}
	}
	return nil
}

// produce writes the archive of b and fills in its method, file, size, and
// encryption.
func (m *Manager) produce(ctx context.Context, b *store.Backup, level string) error {
	recipient, err := m.recipient(ctx)
	if err != nil {
		return err
	}
	if b.ServiceID == "" {
		return m.writeArchive(ctx, b, store.MethodSQLite, "db", level, recipient, func(w io.Writer) error {
			return m.snapshot(ctx, b.ID, w)
		})
	}
	sv, err := m.store.Service(ctx, b.ServiceID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	vols, err := m.store.Volumes(ctx, sv.ID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if len(vols) == 0 {
		return ErrNoVolumes
	}
	d, err := m.store.ActiveDeployment(ctx, sv.ID)
	if errors.Is(err, store.ErrNotFound) {
		return errNotDeployed
	}
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if !isDatabase(sv.Kind) {
		// App volumes are read live, without stopping the app.
		return m.writeArchive(ctx, b, store.MethodVolume, "tar", level, recipient, func(w io.Writer) error {
			return m.readVolumes(ctx, b.ID, d.Image, vols, w)
		})
	}
	running, err := m.containerRunning(ctx, d)
	if err != nil {
		return err
	}
	if !running {
		// Hold the stopped database so that nothing starts it while its
		// files are read.
		held, err := m.services.Hold(ctx, sv.ID)
		if err != nil {
			return fmt.Errorf("backup: hold service: %w", err)
		}
		defer func() {
			if err := held.Release(context.WithoutCancel(ctx)); err != nil {
				m.log.Error("backup: release service", "service", sv.ID, "err", err)
			}
		}()
		if d, err = held.Active(ctx); errors.Is(err, store.ErrNotFound) {
			return errNotDeployed
		} else if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		if running, err = held.Running(ctx); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	if !running {
		return m.writeArchive(ctx, b, store.MethodVolume, "tar", level, recipient, func(w io.Writer) error {
			return m.readVolumes(ctx, b.ID, d.Image, vols, w)
		})
	}
	eng := engines[sv.Kind]
	return m.writeArchive(ctx, b, store.MethodDump, eng.ext, level, recipient, func(w io.Writer) error {
		return execScript(ctx, m.docker, d.ContainerID, sv.Kind+" dump", eng.dump, nil, w)
	})
}

// setMethod records the method a backup ended up using, when the service's
// state changed after the backup was queued.
func (m *Manager) setMethod(ctx context.Context, b *store.Backup, method store.BackupMethod) error {
	if b.Method == method {
		return nil
	}
	b.Method = method
	if err := m.store.UpdateBackup(ctx, *b); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// writeArchive writes the archive of b, produced by fill, to its local file:
// compressed at level, encrypted to recipient if it is not nil, written to a
// partial file, synced, and renamed into place.
func (m *Manager) writeArchive(ctx context.Context, b *store.Backup, method store.BackupMethod, ext, level string,
	recipient age.Recipient, fill func(io.Writer) error) (err error) {
	if err := m.setMethod(ctx, b, method); err != nil {
		return err
	}
	b.File = b.ID + "." + ext + ".zst"
	if recipient != nil {
		b.File += ".age"
	}
	b.Encrypted = recipient != nil

	dir := m.targetDir(b.ServiceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	final := filepath.Join(dir, b.File)
	partial := final + partialSuffix
	f, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(partial)
		}
	}()
	enc, err := newEncoder(f, level, recipient)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := fill(enc); err != nil {
		enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("backup: sync %s: %w", partial, err)
	}
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("backup: close %s: %w", partial, err)
	}
	if err := os.Rename(partial, final); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	syncDir(dir)
	b.Size = info.Size()
	return nil
}

// syncDir makes a rename in dir durable, as far as the file system allows.
func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
}

// snapshot writes a consistent copy of shed.db to w.
func (m *Manager) snapshot(ctx context.Context, id string, w io.Writer) error {
	p := filepath.Join(m.targetDir(""), id+".snapshot"+partialSuffix)
	if err := m.store.Snapshot(ctx, p); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer os.Remove(p)
	f, err := os.Open(p)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("backup: copy snapshot: %w", err)
	}
	return nil
}

// helper creates a helper container labeled with id that mounts the Docker
// volumes named by volName for vols at their mount paths, to read or write
// them with the Docker archive API. It is never started. The returned
// function removes it.
func (m *Manager) helper(ctx context.Context, name, id, image string, vols []store.Volume, volName func(store.Volume) string, readOnly bool) (string, func(), error) {
	if image == "" {
		return "", nil, errors.New("backup: the active deployment has no image")
	}
	mounts := make([]docker.Mount, len(vols))
	for i, v := range vols {
		mounts[i] = docker.Mount{Volume: volName(v), Target: v.MountPath, ReadOnly: readOnly}
	}
	cid, err := m.docker.Create(ctx, docker.RunSpec{
		Name:   name,
		Image:  image,
		Cmd:    []string{"true"}, // Never run; some images have no command.
		Labels: map[string]string{helperLabel: id},
		Mounts: mounts,
	})
	if err != nil {
		return "", nil, fmt.Errorf("backup: create helper container: %w", err)
	}
	return cid, func() { m.removeHelper(context.WithoutCancel(ctx), cid) }, nil
}

// removeHelper removes a helper container. Docker also removes the anonymous
// volumes it created for the VOLUME paths of the container's image, but never
// the service's named volumes.
func (m *Manager) removeHelper(ctx context.Context, cid string) {
	if err := m.docker.Remove(ctx, cid); err != nil {
		m.log.Error("backup: remove helper container", "container", cid, "err", err)
	}
}

// readVolumes writes a tar of vols to w, read through a helper container
// running image.
func (m *Manager) readVolumes(ctx context.Context, backupID, image string, vols []store.Volume, w io.Writer) error {
	cid, remove, err := m.helper(ctx, "shed-backup-"+backupID, backupID, image, vols, volumeName, true)
	if err != nil {
		return err
	}
	defer remove()
	if err := writeVolumes(ctx, m.docker, cid, mountPaths(vols), w); err != nil {
		return fmt.Errorf("backup: archive volumes: %w", err)
	}
	return nil
}

// upload stores the archive of the uploading backup b in the destination d,
// reached through r, and records b as succeeded with the outcome. The intended
// destination and object key are recorded before the upload starts, and the
// remote location is committed before the local file is removed. With
// keep_local 0, a successful upload then removes the local file, so a crash
// at any point leaves a copy that the row points to.
func (m *Manager) upload(ctx context.Context, b store.Backup, p PolicyInput, r Remote, d store.BackupDestination) error {
	key := objectKey(d, b.ServiceID, b.File)
	b.RemoteKey, b.DestinationID = key, d.ID
	if err := m.store.UpdateBackup(context.WithoutCancel(ctx), b); err != nil {
		return fmt.Errorf("backup: record upload target: %w", err)
	}
	err := m.put(ctx, r, key, m.localPath(b))
	finished := m.now()
	b.Status, b.FinishedAt = store.BackupSucceeded, &finished
	if err != nil {
		b.RemoteKey, b.DestinationID, b.RemoteError = "", "", err.Error()
		m.log.Error("backup: upload failed", "backup", b.ID, "err", err)
	} else {
		b.RemoteError = ""
	}
	if err := m.store.UpdateBackup(context.WithoutCancel(ctx), b); err != nil {
		return fmt.Errorf("backup: record upload: %w", err)
	}
	if b.RemoteKey == "" || p.KeepLocal != 0 {
		return nil
	}
	if err := os.Remove(m.localPath(b)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.log.Error("backup: remove uploaded archive", "backup", b.ID, "err", err)
		return nil
	}
	b.Local = false
	if err := m.store.UpdateBackup(context.WithoutCancel(ctx), b); err != nil {
		return fmt.Errorf("backup: record local removal: %w", err)
	}
	return nil
}

// put uploads the file at path as key.
func (m *Manager) put(ctx context.Context, r Remote, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("backup: upload: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("backup: upload: %w", err)
	}
	if err := r.Put(ctx, key, f, info.Size()); err != nil {
		return fmt.Errorf("backup: upload: %w", err)
	}
	return nil
}

// retention decides which scheduled backups lose their local file and which
// lose their S3 object, given a target's backups newest first. Only
// successful scheduled backups are pruned. The newest keepLocal with a local
// file keep it; with keepLocal 0, files of backups that are not in S3 are
// kept, so a failed upload never leaves a backup without a copy. The newest
// keepRemote with an object keep it; objects are pruned only while
// uploading.
func retention(bs []store.Backup, p PolicyInput) (dropLocal, dropRemote map[string]bool) {
	dropLocal, dropRemote = make(map[string]bool), make(map[string]bool)
	local, remote := 0, 0
	for _, b := range bs {
		if b.Trigger != store.BackupSchedule || b.Status != store.BackupSucceeded {
			continue
		}
		if b.Local {
			switch {
			case local < p.KeepLocal:
				local++
			case p.KeepLocal == 0 && b.RemoteKey == "":
			default:
				dropLocal[b.ID] = true
			}
		}
		if b.RemoteKey != "" && p.Upload {
			if remote < p.KeepRemote {
				remote++
			} else {
				dropRemote[b.ID] = true
			}
		}
	}
	return dropLocal, dropRemote
}

// prune applies the retention of policy p to a target's scheduled backups.
// Backups left with neither a local file nor an S3 object are deleted.
func (m *Manager) prune(ctx context.Context, serviceID string, p PolicyInput) error {
	bs, err := m.store.Backups(ctx, serviceID, 0)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	dropLocal, dropRemote := retention(bs, p)
	remotes := make(map[string]Remote) // by destination ID
	var errs []error
	for _, b := range bs {
		changed := false
		if dropLocal[b.ID] {
			if err := os.Remove(m.localPath(b)); err == nil || errors.Is(err, os.ErrNotExist) {
				b.Local, changed = false, true
			} else {
				errs = append(errs, err)
			}
		}
		if dropRemote[b.ID] && b.DestinationID != "" {
			r, ok := remotes[b.DestinationID]
			if !ok {
				var err error
				if r, err = m.remoteOf(ctx, b); err != nil {
					errs = append(errs, err) // Once per destination; r stays nil.
				}
				remotes[b.DestinationID] = r
			}
			if r != nil {
				if err := r.Delete(ctx, b.RemoteKey); err == nil {
					b.RemoteKey, b.DestinationID, changed = "", "", true
				} else {
					errs = append(errs, err)
				}
			}
		}
		var err error
		switch {
		case !changed:
		case !b.Local && b.RemoteKey == "":
			err = m.store.DeleteBackup(ctx, b.ID)
		default:
			err = m.store.UpdateBackup(ctx, b)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// runRestore runs the restore r of backup b and records the outcome.
func (m *Manager) runRestore(ctx context.Context, r store.Restore, b store.Backup) {
	err := m.restore(ctx, r, b)
	finished := m.now()
	r.FinishedAt = &finished
	if err != nil {
		r.Status, r.Error = store.RestoreFailed, failure(ctx, err)
		m.log.Error("restore failed", "restore", r.ID, "backup", b.ID, "service", r.ServiceID, "err", r.Error)
	} else {
		r.Status = store.RestoreSucceeded
		m.log.Info("restore succeeded", "restore", r.ID, "backup", b.ID, "service", r.ServiceID)
	}
	if err := m.store.UpdateRestore(context.WithoutCancel(ctx), r); err != nil && !errors.Is(err, store.ErrNotFound) {
		m.log.Error("backup: record restore", "restore", r.ID, "err", err)
	}
}

// restore puts the archive of b back into its service: it takes a
// pre-restore backup, verifies the archive, holds the service, replaces the
// data, and releases the service.
func (m *Manager) restore(ctx context.Context, r store.Restore, b store.Backup) (err error) {
	sv, err := m.store.Service(ctx, r.ServiceID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	vols, err := m.store.Volumes(ctx, sv.ID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	// 1. Back up the current state, inline: the restore holds the job slot.
	pre, err := m.newBackup(ctx, sv.ID, store.BackupPreRestore)
	if err != nil {
		return fmt.Errorf("pre-restore backup: %w", err)
	}
	if pre, err = m.store.CreateBackup(ctx, pre); err != nil {
		return fmt.Errorf("pre-restore backup: %w", err)
	}
	if err := m.runBackup(ctx, pre); err != nil {
		return fmt.Errorf("pre-restore backup: %w", err)
	}

	// 2. Get the archive and check that it decodes, before touching the
	// service.
	path, cleanup, err := m.fetch(ctx, b, r.ID)
	if err != nil {
		return err
	}
	defer cleanup()
	var identity age.Identity
	if b.Encrypted {
		id, err := m.identity(ctx)
		if err != nil {
			return err
		}
		identity = id
	}
	var present []string
	var size int64
	err = readArchive(path, identity, func(data io.Reader) error {
		if b.Method != store.MethodVolume {
			n, err := io.Copy(io.Discard, data)
			size = n
			return err
		}
		p, unknown, err := scanVolumes(data, mountPaths(vols))
		present = p
		if len(unknown) > 0 {
			m.log.Warn("restore: skipping archived paths that are no volume of the service",
				"restore", r.ID, "paths", unknown)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("backup: check archive: %w", err)
	}
	if b.Method == store.MethodVolume && len(present) == 0 {
		return errors.New("the archive contains none of the service's volumes")
	}

	// 3. Hold the service for the rest of the restore.
	held, err := m.services.Hold(ctx, sv.ID)
	if err != nil {
		return fmt.Errorf("backup: hold service: %w", err)
	}
	defer func() {
		// 6. Release: the service starts again unless it is stopped, by the
		// user or by a restore fence that a failure left in place.
		rerr := held.Release(context.WithoutCancel(ctx))
		switch {
		case rerr == nil:
		case err == nil:
			err = fmt.Errorf("data restored, but starting the service failed: %w", rerr)
		default:
			m.log.Error("backup: release service", "service", sv.ID, "err", rerr)
		}
	}()
	d, err := held.Active(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return errNotDeployed
	}
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	switch {
	case b.Method == store.MethodVolume:
		// 4. Volumes: replace each volume in the archive.
		var restore []store.Volume
		for _, v := range vols {
			if slices.Contains(present, v.MountPath) {
				restore = append(restore, v)
			}
		}
		return m.replaceVolumes(ctx, held, sv.ID, r.ID, d.Image, restore, func(w io.Writer) error {
			return readArchive(path, identity, func(data io.Reader) error {
				return filterVolumes(data, w, present)
			})
		})
	case b.Method == store.MethodDump && sv.Kind == "redis":
		// 4. Redis: the RDB file replaces the volume's data.
		i := slices.IndexFunc(vols, func(v store.Volume) bool { return relMount(v.MountPath) == relMount(redisRDBDir) })
		if i < 0 {
			return fmt.Errorf("the service has no volume at %s for the RDB file", redisRDBDir)
		}
		return m.replaceVolumes(ctx, held, sv.ID, r.ID, d.Image, vols[i:i+1], func(w io.Writer) error {
			return redisFiles(w, size, m.now(), func(rdb io.Writer) error {
				return readArchive(path, identity, func(data io.Reader) error {
					_, err := io.Copy(rdb, data)
					return err
				})
			})
		})
	case b.Method == store.MethodDump:
		// 5. Dumps: load into the running database.
		eng, ok := engines[sv.Kind]
		if !ok || eng.restore == "" {
			return fmt.Errorf("cannot restore a %s dump into a %s service", b.Method, sv.Kind)
		}
		running, err := held.Running(ctx)
		if err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		if !running {
			return errors.New("the service is not running; start it to restore a database dump")
		}
		return m.loadDump(ctx, held, sv.ID, r.ID, func() error {
			return readArchive(path, identity, func(data io.Reader) error {
				return execScript(ctx, m.docker, d.ContainerID, sv.Kind+" restore", eng.restore, data, io.Discard)
			})
		})
	}
	return fmt.Errorf("cannot restore a %s backup", b.Method)
}

// loadDump runs load, which loads a dump into the held service's running
// database, with the service fenced. If the load fails or is canceled, the
// database may hold partial data, and a canceled load may still be running
// in the container, so the service's containers are stopped and removed and
// the service is left stopped. If they cannot be stopped, the fence stays,
// and Recover stops them when shed starts again.
func (m *Manager) loadDump(ctx context.Context, held Held, serviceID, restoreID string, load func() error) error {
	if _, err := m.store.CreateRestoreFence(ctx, store.RestoreFence{
		ServiceID: serviceID, RestoreID: restoreID, Phase: store.RestoreLoading,
	}); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	err := load()
	if err == nil {
		return m.unfence(ctx, serviceID, nil, nil)
	}
	ctx = context.WithoutCancel(ctx)
	if serr := held.StopAndRemove(ctx); serr != nil {
		m.log.Error("restore: stop database after a failed load", "restore", restoreID, "service", serviceID, "err", serr)
		return fmt.Errorf("%w; stopping the database failed too (%v), so the load may still be running; "+
			"the service is left stopped, and shed stops it when it starts again", err, serr)
	}
	if derr := m.store.DeleteRestoreFence(ctx, serviceID); derr != nil {
		m.log.Error("backup: delete restore fence", "service", serviceID, "err", derr)
	}
	return fmt.Errorf("%w; the database may hold partial data, so the service was stopped: "+
		"restore a backup, such as the pre-restore one, or start the service to keep the data as it is", err)
}

// redisFiles writes a tar, to extract at "/", of the redis files that
// restore an RDB file of size bytes written by copyRDB: dump.rdb, and a
// multi-part AOF whose base is the same file.
func redisFiles(w io.Writer, size int64, modTime time.Time, copyRDB func(io.Writer) error) error {
	dir := relMount(redisRDBDir)
	rdb := dir + "/" + redisRDBFile
	aofDir := dir + "/" + redisAOFDir
	tw := tar.NewWriter(w)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: rdb, Mode: 0o644, Size: size, ModTime: modTime}); err != nil {
		return err
	}
	if err := copyRDB(tw); err != nil {
		return err
	}
	hdrs := []*tar.Header{
		{Typeflag: tar.TypeDir, Name: aofDir + "/", Mode: 0o755, ModTime: modTime},
		{Typeflag: tar.TypeLink, Name: aofDir + "/" + redisAOFBase, Linkname: rdb, ModTime: modTime},
		{Typeflag: tar.TypeReg, Name: aofDir + "/" + redisAOFManifest, Mode: 0o644, Size: int64(len(redisManifestBody)), ModTime: modTime},
	}
	for _, h := range hdrs {
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(tw, redisManifestBody); err != nil {
		return err
	}
	return tw.Close()
}

// readArchive decodes the archive file at path and passes the original data
// to read.
func readArchive(path string, identity age.Identity, read func(io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := decode(f, identity)
	if err != nil {
		return err
	}
	defer r.Close()
	return read(r)
}

// fetch returns the path of a local copy of b's archive, downloading it from
// S3 to a temporary file if there is no local one. cleanup removes a
// downloaded copy.
func (m *Manager) fetch(ctx context.Context, b store.Backup, restoreID string) (path string, cleanup func(), err error) {
	if b.Local {
		p := m.localPath(b)
		if _, err := os.Stat(p); err == nil {
			return p, func() {}, nil
		}
	}
	rc, err := m.openArchive(ctx, store.Backup{ID: b.ID, ServiceID: b.ServiceID, RemoteKey: b.RemoteKey, DestinationID: b.DestinationID})
	if err != nil {
		return "", nil, err
	}
	defer rc.Close()
	dir := m.targetDir(b.ServiceID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("backup: %w", err)
	}
	p := filepath.Join(dir, restoreID+".download"+partialSuffix)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("backup: %w", err)
	}
	_, err = io.Copy(f, rc)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(p)
		return "", nil, fmt.Errorf("backup: download %s: %w", b.ID, err)
	}
	return p, func() { os.Remove(p) }, nil
}
