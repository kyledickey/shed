package build

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateArgs(t *testing.T) {
	base := []string{"buildx", "create", "--name", "shed", "--driver", "docker-container", "--bootstrap"}
	tests := []struct {
		name   string
		memory int64
		cpus   float64
		want   []string
	}{
		{"unlimited", 0, 0, base},
		{"memory", 2 << 30, 0, append(slices.Clone(base),
			"--driver-opt", "memory=2147483648", "--driver-opt", "memory-swap=2147483648")},
		{"cpus", 0, 1.5, append(slices.Clone(base),
			"--driver-opt", "cpu-period=100000", "--driver-opt", "cpu-quota=150000")},
		{"tiny cpus", 0, 0.001, append(slices.Clone(base),
			"--driver-opt", "cpu-period=100000", "--driver-opt", "cpu-quota=1000")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := createArgs("shed", tt.memory, tt.cpus); !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckDisk(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name    string
		b       *Builder
		wantErr bool
	}{
		{"disabled", &Builder{WorkDir: dir}, false},
		{"enough", &Builder{WorkDir: dir, MinFree: 1}, false},
		{"low work dir", &Builder{WorkDir: dir, MinFree: math.MaxUint64}, true},
		{"low docker root", &Builder{WorkDir: filepath.Join(dir, "missing"), dockerRoot: dir, MinFree: math.MaxUint64}, true},
		{"missing paths skipped", &Builder{WorkDir: filepath.Join(dir, "missing"), MinFree: math.MaxUint64}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.b.checkDisk(tt.b.MinFree)
			if got := errors.Is(err, ErrLowDisk); got != tt.wantErr {
				t.Errorf("checkDisk() = %v, want ErrLowDisk: %v", err, tt.wantErr)
			}
		})
	}
}

// fakeDocker puts a docker script on PATH that appends its arguments to the
// returned log file and prints root for docker info.
func fakeDocker(t *testing.T, root string) string {
	t.Helper()
	bin := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("CALLS", calls)
	t.Setenv("ROOT", root)
	scripts := map[string]string{
		"git":    "#!/bin/sh\ntouch Dockerfile\n",
		"docker": "#!/bin/sh\necho \"$*\" >> \"$CALLS\"\nif [ \"$1\" = info ]; then echo \"$ROOT\"; fi\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return calls
}

func TestBuildUsesLimitedBuilder(t *testing.T) {
	calls := fakeDocker(t, t.TempDir())
	b := &Builder{WorkDir: t.TempDir(), Instance: "shed", Memory: 1 << 30, CPUs: 1, MinFree: 1}
	req := Request{RepoURL: "https://example.com/r.git", Commit: "abc", Image: "x"}
	for _, id := range []string{"a", "b"} {
		if err := b.Build(context.Background(), id, req, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"buildx rm --keep-state shed", "buildx create --name shed", "info", "buildx build --builder shed", "buildx build --builder shed"}
	if len(lines) != len(want) {
		t.Fatalf("docker calls = %q", lines)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("call %d = %q, want prefix %q", i, lines[i], prefix)
		}
	}
	if !strings.Contains(lines[1], "memory=1073741824") || !strings.Contains(lines[1], "cpu-quota=100000") {
		t.Errorf("builder created without limits: %q", lines[1])
	}
}

func TestBuildRejectsLowDisk(t *testing.T) {
	calls := fakeDocker(t, t.TempDir())
	b := &Builder{WorkDir: t.TempDir(), MinFree: math.MaxUint64}
	req := Request{RepoURL: "https://example.com/r.git", Commit: "abc", Image: "x"}
	if err := b.Build(context.Background(), "a", req, io.Discard); !errors.Is(err, ErrLowDisk) {
		t.Fatalf("Build() = %v, want ErrLowDisk", err)
	}
	data, _ := os.ReadFile(calls)
	if strings.Contains(string(data), "build") {
		t.Errorf("built despite low disk: %q", data)
	}
	if entries, _ := os.ReadDir(b.WorkDir); len(entries) != 0 {
		t.Errorf("workspace created: %v", entries)
	}
}

func TestBuildSlotSerializesAndCancels(t *testing.T) {
	var b Builder
	release, err := b.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter: %v", err)
	}
	release()
	var active atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			release, err := b.acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			if n := active.Add(1); n != 1 {
				t.Errorf("%d active builds", n)
			}
			active.Add(-1)
			release()
		})
	}
	wg.Wait()
}

func TestCanceledCommandKillsChildren(t *testing.T) {
	dir := t.TempDir()
	childDone := filepath.Join(dir, "child-done")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := run(ctx, dir, []string{"CHILD_DONE=" + childDone}, io.Discard, "sh", "-c", `(sleep 0.4; echo orphan > "$CHILD_DONE") & wait`)
	if err == nil {
		t.Fatal("command did not cancel")
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := os.Stat(childDone); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("child survived cancellation: %v", err)
	}
}

func TestQueuedBuildDeadlineDoesNotCreateWorkspace(t *testing.T) {
	b := Builder{WorkDir: t.TempDir()}
	release, err := b.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = b.Build(ctx, "waiting", Request{RepoURL: "https://example.com/r.git", Commit: "abc", Image: "x"}, io.Discard)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	entries, err := os.ReadDir(b.WorkDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("queued build created workspace: %v %v", entries, err)
	}
}

func TestBuildCancelsWhenDiskFills(t *testing.T) {
	bin := t.TempDir()
	scripts := map[string]string{
		"git":    "#!/bin/sh\ntouch Dockerfile\n",
		"docker": "#!/bin/sh\n[ \"$1\" = info ] && exit 0\nexec sleep 30\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var calls atomic.Int32
	b := &Builder{
		WorkDir:  t.TempDir(),
		MinFree:  100 << 20,
		diskPoll: 10 * time.Millisecond,
		statfs: func(string) (uint64, error) {
			// Plenty for the preflight check, then below half of MinFree.
			if calls.Add(1) == 1 {
				return 200 << 20, nil
			}
			return 10 << 20, nil
		},
	}
	req := Request{RepoURL: "https://example.com/r.git", Commit: "abc", Image: "x"}
	start := time.Now()
	err := b.Build(context.Background(), "a", req, io.Discard)
	if !errors.Is(err, ErrLowDisk) {
		t.Fatalf("Build() = %v, want ErrLowDisk", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("build was not killed promptly: %v", time.Since(start))
	}
	if entries, _ := os.ReadDir(b.WorkDir); len(entries) != 0 {
		t.Errorf("workspace left behind: %v", entries)
	}
}
