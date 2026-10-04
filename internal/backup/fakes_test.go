package backup

import (
	"archive/tar"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// Compile-time checks that the real implementations fit.
var (
	_ Store  = (*store.Store)(nil)
	_ Docker = (*docker.Client)(nil)
)

// recorder keeps an ordered log of events across fakes.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) add(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf(format, args...))
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

// fakeDocker keeps containers and named volumes in memory. A named volume
// that was never removed or created holds the seed of its mount path in
// volumes; once removed or created, it holds what CopyTo extracted into it.
type fakeDocker struct {
	rec *recorder

	mu         sync.Mutex
	containers map[string]docker.Container
	mounts     map[string][]docker.Mount // by container ID
	next       int
	dump       string // what dump scripts print
	execErr    error  // returned by Exec after reading stdin
	// restoreHook is called by restore execs after reading stdin; an error
	// aborts the exec, as canceling its context does.
	restoreHook func(ctx context.Context) error
	restored    string            // stdin of the last restore script
	volumes     map[string][]byte // seed CopyFrom tar by mount path
	data        map[string]map[string]fakeFile
	touched     map[string]bool         // named volumes removed or created
	copyToErr   error                   // returned by every CopyTo
	copyToHook  func(name string) error // called by CopyTo with the container name
	lossy       map[string]bool         // CopyTo into containers by name drops files
	extracted   []byte                  // last tar given to CopyTo
	// imageVolume makes created containers get an anonymous volume, as
	// for images that declare a VOLUME.
	imageVolume bool
	anonymous   map[string]bool // anonymous volumes not yet removed
}

// fakeFile is an entry of a named volume, by its path in the volume.
type fakeFile struct {
	hdr  tar.Header
	body []byte
}

func newFakeDocker(rec *recorder) *fakeDocker {
	return &fakeDocker{
		rec: rec, containers: make(map[string]docker.Container), mounts: make(map[string][]docker.Mount),
		anonymous: make(map[string]bool), volumes: make(map[string][]byte),
		data: make(map[string]map[string]fakeFile), touched: make(map[string]bool),
		lossy: make(map[string]bool),
	}
}

// files returns the entries of the named volume mounted at mountPath, or nil
// if it does not exist. f.mu must be held.
func (f *fakeDocker) files(name, mountPath string) map[string]fakeFile {
	if f.touched[name] {
		return f.data[name]
	}
	seed, ok := f.volumes[mountPath]
	if !ok {
		return nil
	}
	files := map[string]fakeFile{}
	base := path.Base(mountPath)
	tr := tar.NewReader(bytes.NewReader(seed))
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		body, _ := io.ReadAll(tr)
		rel := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSuffix(h.Name, "/"), base), "/")
		files[rel] = fakeFile{hdr: *h, body: body}
	}
	return files
}

// mountOf returns the mount of container id that holds the path p, relative
// to "/", and p relative to the mount.
func (f *fakeDocker) mountOf(id, p string) (docker.Mount, string, bool) {
	var best docker.Mount
	found := false
	for _, m := range f.mounts[id] {
		if under(p, relMount(m.Target)) && (!found || len(relMount(m.Target)) > len(relMount(best.Target))) {
			best, found = m, true
		}
	}
	if !found {
		return docker.Mount{}, "", false
	}
	return best, strings.TrimPrefix(strings.TrimPrefix(p, relMount(best.Target)), "/"), true
}

// volumeFiles returns the entries of the named volume name, for tests.
func (f *fakeDocker) volumeFiles(name string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for rel, file := range f.data[name] {
		if file.hdr.Typeflag == tar.TypeReg {
			out[rel] = string(file.body)
		}
	}
	return out
}

func (f *fakeDocker) Inspect(_ context.Context, id string) (docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return docker.Container{}, fmt.Errorf("container %s: %w", id, cerrdefs.ErrNotFound)
	}
	return c, nil
}

func (f *fakeDocker) Exec(ctx context.Context, id string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	script := cmd[len(cmd)-1]
	kind := "dump"
	if stdin != nil {
		kind = "restore"
		b, err := io.ReadAll(stdin)
		if err != nil {
			return err
		}
		f.mu.Lock()
		f.restored = string(b)
		hook := f.restoreHook
		f.mu.Unlock()
		if hook != nil {
			if err := hook(ctx); err != nil {
				f.rec.add("exec %s %s aborted", id, kind)
				return err
			}
		}
	}
	f.rec.add("exec %s %s", id, kind)
	f.mu.Lock()
	dump, err := f.dump, f.execErr
	f.mu.Unlock()
	if err != nil {
		io.WriteString(stderr, "pg_dump: error: connection refused\n")
		return err
	}
	if stdin == nil && strings.Contains(script, "dump") {
		io.WriteString(stdout, dump)
	}
	return nil
}

func (f *fakeDocker) Stop(_ context.Context, id string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.containers[id]
	if !ok {
		return fmt.Errorf("container %s: %w", id, cerrdefs.ErrNotFound)
	}
	c.Running = false
	f.containers[id] = c
	f.rec.add("stop %s", id)
	return nil
}

func (f *fakeDocker) Create(_ context.Context, spec docker.RunSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("helper%d", f.next)
	var mounts, vols []string
	for _, m := range spec.Mounts {
		mounts = append(mounts, fmt.Sprintf("%s:%s:ro=%v", m.Volume, m.Target, m.ReadOnly))
		vols = append(vols, m.Volume)
	}
	if f.imageVolume {
		vols = append(vols, "anon-"+id)
		f.anonymous["anon-"+id] = true
	}
	f.containers[id] = docker.Container{ID: id, Name: spec.Name, Labels: spec.Labels, Volumes: vols}
	f.mounts[id] = spec.Mounts
	f.rec.add("create %s %s %s", spec.Name, spec.Image, strings.Join(mounts, ","))
	return id, nil
}

func (f *fakeDocker) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, v := range f.containers[id].Volumes {
		delete(f.anonymous, v) // Like RemoveVolumes: named volumes stay.
	}
	delete(f.containers, id)
	delete(f.mounts, id)
	f.rec.add("remove %s", id)
	return nil
}

func (f *fakeDocker) List(_ context.Context, labels map[string]string) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []docker.Container
	for _, c := range f.containers {
		match := true
		for k, v := range labels {
			if got, ok := c.Labels[k]; !ok || v != "" && got != v {
				match = false
			}
		}
		if match {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeDocker) CopyFrom(_ context.Context, id, p string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, rel, ok := f.mountOf(id, relMount(p))
	if !ok || rel != "" {
		return nil, fmt.Errorf("no volume at %s: %w", p, cerrdefs.ErrNotFound)
	}
	files := f.files(m.Volume, m.Target)
	if files == nil {
		return nil, fmt.Errorf("no volume at %s: %w", p, cerrdefs.ErrNotFound)
	}
	f.rec.add("copy from %s %s", id, p)
	base := path.Base(m.Target)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	root, ok := files[""]
	if !ok {
		root = fakeFile{hdr: tar.Header{Typeflag: tar.TypeDir, Mode: 0o755}}
	}
	root.hdr.Name = base + "/"
	tw.WriteHeader(&root.hdr)
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		if rel == "" {
			continue
		}
		file := files[rel]
		file.hdr.Name = base + "/" + rel
		tw.WriteHeader(&file.hdr)
		tw.Write(file.body)
	}
	tw.Close()
	return io.NopCloser(&buf), nil
}

func (f *fakeDocker) CopyTo(_ context.Context, id, dir string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	name, hook := f.containers[id].Name, f.copyToHook
	f.mu.Unlock()
	if hook != nil {
		err = hook(name)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rec.add("copy to %s %s", id, dir)
	f.extracted = data
	if err := cmp.Or(f.copyToErr, err); err != nil {
		return err
	}
	lossy := f.lossy[name]
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(tr)
		name := path.Clean(path.Join(strings.TrimPrefix(dir, "/"), h.Name))
		m, rel, ok := f.mountOf(id, name)
		if !ok {
			continue
		}
		files := f.files(m.Volume, m.Target)
		if files == nil {
			return fmt.Errorf("no volume %s", m.Volume)
		}
		if h.Typeflag == tar.TypeLink {
			// Store hard links as copies, like an archive of them reads.
			tm, trel, _ := f.mountOf(id, path.Clean(h.Linkname))
			target := f.files(tm.Volume, tm.Target)[trel]
			h, body = &target.hdr, target.body
		}
		if lossy && h.Typeflag == tar.TypeReg {
			continue
		}
		files[rel] = fakeFile{hdr: *h, body: body}
		f.data[m.Volume], f.touched[m.Volume] = files, true
	}
}

func (f *fakeDocker) EnsureVolume(_ context.Context, name string) error {
	f.rec.add("ensure volume %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.touched[name] && f.data[name] == nil {
		f.data[name] = map[string]fakeFile{}
	}
	return nil
}

func (f *fakeDocker) RemoveVolume(_ context.Context, name string) error {
	f.rec.add("remove volume %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched[name] = true
	delete(f.data, name)
	return nil
}

// volumeTar returns a Docker archive of a mount path holding files.
func volumeTar(mountPath string, files map[string]string) []byte {
	base := path.Base(mountPath)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: base + "/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: base + "/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
		io.WriteString(tw, body)
	}
	tw.Close()
	return buf.Bytes()
}

// fakeServices hands out holds and records what they do.
type fakeServices struct {
	rec      *recorder
	st       *store.Store
	docker   *fakeDocker
	holdErr  error
	stopErr  error // returned by StopAndRemove
	released int
	mu       sync.Mutex
}

func (s *fakeServices) Hold(_ context.Context, id string) (Held, error) {
	if s.holdErr != nil {
		return nil, s.holdErr
	}
	s.rec.add("hold %s", id)
	return &fakeHeld{s: s, id: id}, nil
}

type fakeHeld struct {
	s  *fakeServices
	id string
}

func (h *fakeHeld) Active(ctx context.Context) (store.Deployment, error) {
	return h.s.st.ActiveDeployment(ctx, h.id)
}

func (h *fakeHeld) Running(ctx context.Context) (bool, error) {
	d, err := h.Active(ctx)
	if err != nil {
		return false, err
	}
	c, err := h.s.docker.Inspect(ctx, d.ContainerID)
	if err != nil {
		return false, nil
	}
	return c.Running, nil
}

func (h *fakeHeld) StopAndRemove(ctx context.Context) error {
	d, err := h.Active(ctx)
	if err != nil {
		return err
	}
	if h.s.stopErr != nil {
		h.s.rec.add("stop and remove %s failed", h.id)
		return h.s.stopErr
	}
	h.s.rec.add("stop and remove %s", h.id)
	h.s.docker.mu.Lock()
	delete(h.s.docker.containers, d.ContainerID)
	h.s.docker.mu.Unlock()
	return nil
}

func (h *fakeHeld) Release(context.Context) error {
	h.s.rec.add("release %s", h.id)
	h.s.mu.Lock()
	h.s.released++
	h.s.mu.Unlock()
	return nil
}

// fakeRemote is an in-memory bucket.
type fakeRemote struct {
	mu      sync.Mutex
	objects map[string][]byte
	putErr  error
	checked []string
	// putting, if set, receives a value when Put starts and then Put waits
	// for proceed.
	putting, proceed chan struct{}
}

func (r *fakeRemote) Put(_ context.Context, key string, rd io.Reader, size int64) error {
	if r.putting != nil {
		r.putting <- struct{}{}
		<-r.proceed
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return err
	}
	if int64(len(b)) != size {
		return fmt.Errorf("put %s: got %d bytes, want %d", key, len(b), size)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.putErr != nil {
		return r.putErr
	}
	r.objects[key] = b
	return nil
}

func (r *fakeRemote) Get(_ context.Context, key string) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (r *fakeRemote) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.objects, key)
	return nil
}

func (r *fakeRemote) Check(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checked = append(r.checked, key)
	return r.putErr
}

func (r *fakeRemote) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keys []string
	for k := range r.objects {
		keys = append(keys, k)
	}
	return keys
}
