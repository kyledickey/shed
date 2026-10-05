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

// diskPollInterval is how often a running build's free space is checked.
const diskPollInterval = 3 * time.Second

// freeBytes returns the bytes available to unprivileged writers on the
// filesystem containing path.
func freeBytes(path string) (uint64, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return 0, err
	}
	return fs.Bavail * uint64(fs.Bsize), nil
}

// checkDisk returns ErrLowDisk if the work directory's or Docker's
// filesystem has less than min bytes available. Paths that do not exist
// on this host, such as the root of a remote Docker daemon, are skipped.
func (b *Builder) checkDisk(min uint64) error {
	if b.MinFree == 0 {
		return nil
	}
	statfs := b.statfs
	if statfs == nil {
		statfs = freeBytes
	}
	for _, path := range []string{b.WorkDir, b.dockerRoot} {
		if path == "" {
			continue
		}
		free, err := statfs(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("build: statfs %s: %w", path, err)
		}
		if free < min {
			return fmt.Errorf("%w: %d MiB free on %s, need %d MiB", ErrLowDisk, free>>20, path, min>>20)
		}
	}
	return nil
}

// watchDisk polls free space until ctx is done and calls cancel with an
// ErrLowDisk error when it falls below half of MinFree. Statfs errors are
// ignored, since the preflight check already ran.
func (b *Builder) watchDisk(ctx context.Context, cancel context.CancelCauseFunc) {
	if b.MinFree == 0 {
		return
	}
	interval := b.diskPoll
	if interval <= 0 {
		interval = diskPollInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := b.checkDisk(b.MinFree / 2); errors.Is(err, ErrLowDisk) {
				cancel(fmt.Errorf("%w (build canceled while running; limit is half of build.min_free_mb)", err))
				return
			}
		}
	}
}
