package backup

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// preRestoreName returns the name of the Docker volume that keeps the data
// of a volume while a restore replaces it.
func preRestoreName(v store.Volume) string { return volumeName(v) + "-pre-restore" }

// replaceVolumes replaces the data of the held service's volumes vols with
// the tar written by fill, extracted at "/". It fences the service, so that
// it stays stopped across restarts, stops it, copies each volume to its
// pre-restore volume and checks the copy, and only then empties the volume,
// extracts the tar into it, and checks it. If that fails, the copies are put
// back. On success and on failures that
// leave the data as it was, the fence is lifted, so the release of the hold
// starts the service again unless the user had stopped it. Otherwise the
// service is left stopped and fenced, and Recover puts the previous data back
// when shed starts.
func (m *Manager) replaceVolumes(ctx context.Context, held Held, serviceID, restoreID, image string, vols []store.Volume, fill func(io.Writer) error) error {
	f := store.RestoreFence{ServiceID: serviceID, RestoreID: restoreID, Phase: store.RestoreRetaining, Image: image}
	for _, v := range vols {
		f.VolumeIDs = append(f.VolumeIDs, v.ID)
	}
	if _, err := m.store.CreateRestoreFence(ctx, f); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := held.StopAndRemove(ctx); err != nil {
		return m.unfence(ctx, serviceID, vols, fmt.Errorf("backup: stop service: %w", err))
	}
	if err := m.retain(ctx, restoreID, image, vols); err != nil {
		return m.unfence(ctx, serviceID, vols, err)
	}
	if err := m.store.SetRestorePhase(ctx, serviceID, store.RestoreReplacing); err != nil {
		return m.unfence(ctx, serviceID, vols, fmt.Errorf("backup: %w", err))
	}
	if err := m.extract(ctx, restoreID, image, vols, fill); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%w; the service stays stopped until shed starts again and puts its previous data back", err)
		}
		if perr := m.putBack(context.WithoutCancel(ctx), restoreID, image, vols); perr != nil {
			m.log.Error("restore: put back previous data", "restore", restoreID, "service", serviceID, "err", perr)
			return fmt.Errorf("%w; putting the previous data back failed too (%v), so the service is left stopped "+
				"and its previous data is kept in the volumes %s", err, perr, strings.Join(preRestoreNames(vols), ", "))
		}
		return m.unfence(ctx, serviceID, vols, fmt.Errorf("%w; the previous data was put back", err))
	}
	return m.unfence(ctx, serviceID, vols, nil)
}

// preRestoreNames returns the pre-restore volume names of vols.
func preRestoreNames(vols []store.Volume) []string {
	names := make([]string, len(vols))
	for i, v := range vols {
		names[i] = preRestoreName(v)
	}
	return names
}

// unfence lifts the fence of a service whose volumes vols hold complete
// data, and removes their pre-restore volumes. It returns cause, joined with
// the error of lifting the fence if that fails; the pre-restore volumes are
// then kept.
func (m *Manager) unfence(ctx context.Context, serviceID string, vols []store.Volume, cause error) error {
	ctx = context.WithoutCancel(ctx)
	if err := m.store.LiftRestoreFence(ctx, serviceID); err != nil {
		return errors.Join(cause, fmt.Errorf("backup: the service is left stopped: %w", err))
	}
	for _, name := range preRestoreNames(vols) {
		if err := m.docker.RemoveVolume(ctx, name); err != nil {
			m.log.Error("backup: remove pre-restore volume", "volume", name, "err", err)
		}
	}
	return cause
}

// emptyVolumes removes the Docker volumes named by name for vols and creates
// them again, empty.
func (m *Manager) emptyVolumes(ctx context.Context, vols []store.Volume, name func(store.Volume) string) error {
	for _, v := range vols {
		if err := m.docker.RemoveVolume(ctx, name(v)); err != nil {
			return fmt.Errorf("backup: empty volume %s: %w", name(v), err)
		}
		if err := m.docker.EnsureVolume(ctx, name(v)); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return nil
}

// retain copies the data of vols to their pre-restore volumes and checks the
// copies.
func (m *Manager) retain(ctx context.Context, restoreID, image string, vols []store.Volume) error {
	if err := m.emptyVolumes(ctx, vols, preRestoreName); err != nil {
		return err
	}
	if err := m.copyVolumes(ctx, restoreID, image, vols, volumeName, preRestoreName); err != nil {
		return fmt.Errorf("backup: copy the current data aside: %w", err)
	}
	return nil
}

// putBack replaces the data of vols with that of their pre-restore volumes
// and checks it.
func (m *Manager) putBack(ctx context.Context, restoreID, image string, vols []store.Volume) error {
	if err := m.emptyVolumes(ctx, vols, volumeName); err != nil {
		return err
	}
	if err := m.copyVolumes(ctx, restoreID, image, vols, preRestoreName, volumeName); err != nil {
		return fmt.Errorf("backup: put the previous data back: %w", err)
	}
	return nil
}

// extract empties vols, extracts the tar written by fill into them, and
// checks that they hold everything it contains.
func (m *Manager) extract(ctx context.Context, restoreID, image string, vols []store.Volume, fill func(io.Writer) error) error {
	if err := m.emptyVolumes(ctx, vols, volumeName); err != nil {
		return err
	}
	cid, remove, err := m.helper(ctx, "shed-restore-"+restoreID, restoreID, image, vols, volumeName, false)
	if err != nil {
		return err
	}
	defer remove()
	want, err := m.load(ctx, cid, fill)
	if err != nil {
		return fmt.Errorf("backup: extract archive: %w", err)
	}
	if err := m.verify(ctx, cid, vols, want); err != nil {
		return fmt.Errorf("backup: extract archive: %w", err)
	}
	return nil
}

// copyVolumes copies the data of vols from the Docker volumes named by from
// to the empty ones named by to, and checks the copy.
func (m *Manager) copyVolumes(ctx context.Context, restoreID, image string, vols []store.Volume, from, to func(store.Volume) string) error {
	src, removeSrc, err := m.helper(ctx, "shed-restore-"+restoreID+"-src", restoreID, image, vols, from, true)
	if err != nil {
		return err
	}
	defer removeSrc()
	dst, removeDst, err := m.helper(ctx, "shed-restore-"+restoreID+"-dst", restoreID, image, vols, to, false)
	if err != nil {
		return err
	}
	defer removeDst()
	want, err := m.load(ctx, dst, func(w io.Writer) error {
		return writeVolumes(ctx, m.docker, src, mountPaths(vols), w)
	})
	if err != nil {
		return err
	}
	return m.verify(ctx, dst, vols, want)
}

// load extracts the tar written by fill at "/" in the helper container cid
// and returns its manifest.
func (m *Manager) load(ctx context.Context, cid string, fill func(io.Writer) error) (manifest, error) {
	var want manifest
	err := tee(fill,
		func(r io.Reader) error { return m.docker.CopyTo(ctx, cid, "/", r) },
		func(r io.Reader) (err error) {
			want, err = readManifest(r)
			return err
		})
	return want, err
}

// verify reads back the volumes vols of the helper container cid and checks
// that they hold every entry of want. Other entries are allowed: Docker may
// fill an empty volume with what the image has at its mount path.
func (m *Manager) verify(ctx context.Context, cid string, vols []store.Volume, want manifest) error {
	var got manifest
	err := tee(func(w io.Writer) error {
		return writeVolumes(ctx, m.docker, cid, mountPaths(vols), w)
	}, func(r io.Reader) (err error) {
		got, err = readManifest(r)
		return err
	})
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if bad := want.missing(got); len(bad) > 0 {
		return fmt.Errorf("check: %d of %d entries are missing or differ, such as %q", len(bad), len(want), bad[0])
	}
	return nil
}

// recoverFence finishes a restore that a restart of shed interrupted while
// its service was fenced. Volumes whose replacement may have begun get their
// previous data back. The fence is lifted once the volumes hold complete
// data; if that cannot be done, the service stays stopped and fenced.
func (m *Manager) recoverFence(ctx context.Context, f store.RestoreFence) error {
	sv, err := m.store.Service(ctx, f.ServiceID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	all, err := m.store.Volumes(ctx, f.ServiceID)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	vols := slices.DeleteFunc(all, func(v store.Volume) bool { return !slices.Contains(f.VolumeIDs, v.ID) })
	if !sv.Stopped {
		// Only a shed that did not enforce fences could start a fenced
		// service. Its data may have changed since, so it is neither put
		// back nor kept: the service is stopped, and stays fenced until the
		// user clears the fence.
		if err := m.stopActive(ctx, f.ServiceID); err != nil {
			return err
		}
		if err := m.store.SetServiceStopped(ctx, f.ServiceID, true); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
		return fmt.Errorf("backup: service was started during an unfinished restore; it was stopped and stays fenced, "+
			"and its previous data is kept in the volumes %s", strings.Join(preRestoreNames(vols), ", "))
	}
	switch f.Phase {
	case store.RestoreLoading:
		// The database may hold part of the dump, and the load may still be
		// running in its container. Stop it and leave the service stopped.
		if err := m.stopActive(ctx, f.ServiceID); err != nil {
			return fmt.Errorf("%w; the service stays stopped", err)
		}
		m.log.Warn("restore: a restart interrupted loading a dump; the service stays stopped",
			"service", f.ServiceID, "restore", f.RestoreID)
		return m.store.DeleteRestoreFence(ctx, f.ServiceID)
	case store.RestoreReplacing:
		if err := m.putBack(ctx, f.RestoreID, f.Image, vols); err != nil {
			return fmt.Errorf("%w; the service stays stopped, and its previous data is kept in the volumes %s",
				err, strings.Join(preRestoreNames(vols), ", "))
		}
		m.log.Info("restore: put back the data of an interrupted restore", "service", f.ServiceID, "restore", f.RestoreID)
	}
	return m.unfence(ctx, f.ServiceID, vols, nil)
}

// stopTimeout is how long a database stopped by Recover gets to exit before
// it is killed.
const stopTimeout = 30 * time.Second

// stopActive stops the container of a service's active deployment, if it
// has one.
func (m *Manager) stopActive(ctx context.Context, serviceID string) error {
	d, err := m.store.ActiveDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) || err == nil && d.ContainerID == "" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := m.docker.Stop(ctx, d.ContainerID, stopTimeout); err != nil && !docker.IsNotFound(err) {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// readManifest reads a whole tar and returns its manifest.
func readManifest(r io.Reader) (manifest, error) {
	out := make(manifest)
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			out[name] = "dir"
		case tar.TypeReg:
			sum := sha256.New()
			n, err := io.Copy(sum, tr)
			if err != nil {
				return nil, fmt.Errorf("read archive: %w", err)
			}
			out[name] = fmt.Sprintf("file %d %x", n, sum.Sum(nil))
		case tar.TypeLink:
			target, err := cleanName(h.Linkname)
			if err != nil {
				return nil, err
			}
			desc, ok := out[target]
			if !ok || !strings.HasPrefix(desc, "file ") {
				return nil, fmt.Errorf("archive entry %q links to %q, which is not a file before it", h.Name, h.Linkname)
			}
			out[name] = desc
		case tar.TypeSymlink:
			out[name] = "symlink " + h.Linkname
		default:
			out[name] = fmt.Sprintf("type %q", h.Typeflag)
		}
	}
}

// manifest describes the entries of a volume tar by cleaned name: their
// type, and the size and SHA-256 of regular files. A hard link is described
// as the file it links to, so it does not matter which of the names an
// archiver stored as the file.
type manifest map[string]string

// missing returns the entries of want that got lacks or describes
// differently, sorted.
func (want manifest) missing(got manifest) []string {
	var out []string
	for name, desc := range want {
		if got[name] != desc {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// tee runs fill with a writer that copies what it writes to every sink. Each
// sink reads its own stream concurrently, and whatever a sink leaves unread
// when it returns is discarded. A failed sink's error makes fill's writes
// fail, and fill's error makes the sinks' reads fail. tee returns fill's
// error unless a sink caused it, and otherwise the first error of the sinks.
func tee(fill func(io.Writer) error, sinks ...func(io.Reader) error) error {
	ws := make([]io.Writer, len(sinks))
	pws := make([]*io.PipeWriter, len(sinks))
	errcs := make([]chan error, len(sinks))
	for i, sink := range sinks {
		pr, pw := io.Pipe()
		ws[i], pws[i], errcs[i] = pw, pw, make(chan error, 1)
		go func() {
			err := sink(pr)
			if err != nil {
				pr.CloseWithError(err)
			} else {
				_, _ = io.Copy(io.Discard, pr)
			}
			errcs[i] <- err
		}()
	}
	err := fill(io.MultiWriter(ws...))
	for _, pw := range pws {
		pw.CloseWithError(err)
	}
	var sinkErrs []error
	for _, c := range errcs {
		if serr := <-c; serr != nil {
			sinkErrs = append(sinkErrs, serr)
		}
	}
	if err != nil && !slices.ContainsFunc(sinkErrs, func(serr error) bool { return errors.Is(err, serr) }) {
		return err
	}
	if len(sinkErrs) > 0 {
		return sinkErrs[0]
	}
	return nil
}
