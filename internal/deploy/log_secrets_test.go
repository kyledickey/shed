package deploy

import (
	"context"
	"strings"
	"testing"
	"time"
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
	if err := f.d.RuntimeLogs(ctx, f.svc.ID, 100, &runtime); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runtime.String(), secret) || !strings.Contains(runtime.String(), "password=***") {
		t.Fatalf("runtime secret redaction failed: %q", runtime.String())
	}
}
