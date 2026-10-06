package deploy

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/store"
)

func TestContainerLogsRedactServiceSecrets(t *testing.T) {
	f := newFixture(t)
	const secret = "sensitive-credential"
	if err := f.st.SetVariables(context.Background(), f.svc.ID, map[string]string{"PASSWORD": secret}); err != nil {
		t.Fatal(err)
	}
	f.docker.output = "2026-10-04T10:00:00Z password=" + secret + "\n"
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	log := f.buildLog(t, dep.ID)
	if strings.Contains(log, secret) || !strings.Contains(log, "password=***") {
		t.Fatalf("startup secret redaction failed: %q", log)
	}
	var runtime strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.d.RuntimeLogs(ctx, f.svc.ID, 100, true, &runtime); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runtime.String(), secret) || !strings.Contains(runtime.String(), "password=***") {
		t.Fatalf("runtime secret redaction failed: %q", runtime.String())
	}
}

func TestBuildLogTailMasksSavedValues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	// A value saved after the deployment, which its log could not mask.
	const later = "saved-after-the-build"
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"TOKEN": later}); err != nil {
		t.Fatal(err)
	}
	input := "one\ntwo " + later + "\n" + strings.Repeat("x", maxLine+10) + "\nlast"
	if err := os.WriteFile(f.d.logPath(dep.ID), []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := f.d.BuildLog(ctx, dep.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"two ***", strings.Repeat("x", maxLine), "xxxxxxxxxx", "last"}
	if !slices.Equal(got, want) {
		t.Errorf("BuildLog(4) = %.80q, want %.80q", got, want)
	}
	if got, err := f.d.BuildLog(ctx, dep.ID, 1); err != nil || !slices.Equal(got, []string{"last"}) {
		t.Errorf("BuildLog(1) = %q, %v; want [last]", got, err)
	}
	if _, err := f.d.BuildLog(ctx, "nope", 10); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("BuildLog(nope) error = %v, want not found", err)
	}
}

func TestRuntimeLogsSnapshot(t *testing.T) {
	f := newFixture(t)
	f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	f.docker.mu.Lock()
	f.docker.output = "ready\n"
	f.docker.mu.Unlock()

	// Without follow, RuntimeLogs returns although the container runs and
	// ctx never ends.
	var out strings.Builder
	if err := f.d.RuntimeLogs(context.Background(), f.svc.ID, 10, false, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "ready\n" {
		t.Errorf("output = %q, want ready", out.String())
	}
}
