package build

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
