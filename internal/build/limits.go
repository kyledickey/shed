package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const buildTimeout = 30 * time.Minute

// ErrLowDisk is returned when a filesystem a build writes to has less free
// space than [Builder.MinFree].
var ErrLowDisk = errors.New("build: not enough free disk space")

// cpuPeriod is the CFS period, in microseconds, of the builder's CPU quota.
const cpuPeriod = 100000

// acquire serializes builds across services using the shared Builder.
func (b *Builder) acquire(ctx context.Context) (func(), error) {
	b.once.Do(func() { b.slot = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case b.slot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-b.slot
			return nil, err
		}
		return func() { <-b.slot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// setup recreates the dedicated builder instance with the configured limits
// and finds Docker's root directory, once per Builder. It must be called
// while holding the build slot.
func (b *Builder) setup(ctx context.Context) error {
	if b.ready {
		return nil
	}
	if b.Instance != "" {
		// Limits only apply when the builder container is created, so
		// replace any earlier instance. Its build cache is kept.
		_ = run(ctx, "", nil, io.Discard, "docker", "buildx", "rm", "--keep-state", b.Instance)
		var out bytes.Buffer
		if err := run(ctx, "", nil, &out, "docker", createArgs(b.Instance, b.Memory, b.CPUs)...); err != nil {
			return fmt.Errorf("build: create builder %s: %w: %s", b.Instance, err, bytes.TrimSpace(out.Bytes()))
		}
	}
	if b.MinFree > 0 {
		var out bytes.Buffer
		if err := run(ctx, "", nil, &out, "docker", "info", "--format", "{{.DockerRootDir}}"); err == nil {
			b.dockerRoot = strings.TrimSpace(out.String())
		}
	}
	b.ready = true
	return nil
}

// createArgs returns the docker arguments that create a builder instance
// with the docker-container driver, limited to memory bytes and cpus cores.
func createArgs(instance string, memory int64, cpus float64) []string {
	args := []string{"buildx", "create", "--name", instance, "--driver", "docker-container", "--bootstrap"}
	if memory > 0 {
		m := strconv.FormatInt(memory, 10)
		args = append(args, "--driver-opt", "memory="+m, "--driver-opt", "memory-swap="+m)
	}
	if cpus > 0 {
		quota := max(int64(cpus*cpuPeriod), 1000)
		args = append(args, "--driver-opt", "cpu-period="+strconv.Itoa(cpuPeriod),
			"--driver-opt", "cpu-quota="+strconv.FormatInt(quota, 10))
	}
	return args
}

// checkDisk returns ErrLowDisk if the work directory's or Docker's
// filesystem has less than MinFree bytes available. Paths that do not exist
// on this host, such as the root of a remote Docker daemon, are skipped.
func (b *Builder) checkDisk() error {
	if b.MinFree == 0 {
		return nil
	}
	for _, path := range []string{b.WorkDir, b.dockerRoot} {
		if path == "" {
			continue
		}
		var fs syscall.Statfs_t
		if err := syscall.Statfs(path, &fs); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("build: statfs %s: %w", path, err)
		}
		if free := fs.Bavail * uint64(fs.Bsize); free < b.MinFree {
			return fmt.Errorf("%w: %d MiB free on %s, need %d MiB", ErrLowDisk, free>>20, path, b.MinFree>>20)
		}
	}
	return nil
}
