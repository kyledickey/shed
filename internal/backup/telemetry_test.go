package backup

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/telemetry/telemetrytest"
)

func TestBackupsAreTracedAndCounted(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newEnv(t)
	sv := e.service("postgres", true, "/var/lib/postgresql")
	e.docker.dump = "-- SQL dump\n"
	e.backUp(sv.ID)
	e.docker.execErr = &docker.ExitError{Code: 1}
	if _, err := e.m.BackUp(e.ctx, sv.ID); err != nil {
		t.Fatal(err)
	}
	e.drain()

	root := rec.Span("backup")
	if root == nil {
		t.Fatal("no backup span")
	}
	if got := telemetrytest.Attr(root, "shed.service.id"); got != sv.ID {
		t.Errorf("backup span shed.service.id = %q, want %q", got, sv.ID)
	}
	step := rec.Span("write archive")
	if step == nil || step.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Error("no write archive step under the backup span")
	}

	labels := []attribute.KeyValue{
		attribute.String("shed_service_id", sv.ID),
		attribute.String("shed_service_kind", "postgres"),
		attribute.String("shed_project_name", "p"),
		attribute.String("kind", "backup"),
	}
	for outcome, want := range map[string]int64{"succeeded": 1, "failed": 1} {
		if n := rec.Sum(t, "shed_backups_total", append(labels, attribute.String("outcome", outcome))...); n != want {
			t.Errorf("shed_backups_total{outcome=%q} = %d, want %d", outcome, n, want)
		}
	}
	if n := rec.Count(t, "shed_backup_duration_seconds", labels...); n != 2 {
		t.Errorf("shed_backup_duration_seconds count = %d, want 2", n)
	}
	if n := rec.Count(t, "shed_backup_bytes", attribute.String("shed_service_id", sv.ID)); n != 1 {
		t.Errorf("shed_backup_bytes count = %d, want 1 for the successful backup", n)
	}
}

func TestSystemBackupsCarryShedLabels(t *testing.T) {
	rec := telemetrytest.Install(t)
	e := newEnv(t)
	e.backUp("")
	if n := rec.Sum(t, "shed_backups_total", attribute.String("kind", "backup"), attribute.String("outcome", "succeeded")); n != 1 {
		t.Fatalf("shed_backups_total = %d, want 1", n)
	}
	if n := rec.Sum(t, "shed_backups_total", attribute.String("shed_service_id", "")); n != 0 {
		t.Error("a shed.db backup carries an empty service label, which would hide shed's own")
	}
}
