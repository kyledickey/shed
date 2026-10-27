package observability

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/kyledickey/shed/internal/otlp"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/victoria"
)

// auditCursorKey is the settings row holding the ID of the last audit event
// VictoriaLogs acknowledged.
const auditCursorKey = "audit_mirror_cursor"

// AuditMirrorStore reads the audit log and keeps the mirror's cursor.
// *store.Store implements it.
type AuditMirrorStore interface {
	AuditEventsAfter(ctx context.Context, after int64, limit int) ([]store.AuditEvent, error)
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

var _ AuditMirrorStore = (*store.Store)(nil)

// AuditMirror copies audit events into VictoriaLogs, resuming after the
// last one the store acknowledged, so it catches up after any outage.
type AuditMirror struct {
	store AuditMirrorStore
	logs  LogWriter
	log   *slog.Logger

	every     time.Duration // how often new events are looked for, and the wait after a failure
	batchSize int
}

// NewAuditMirror returns an AuditMirror. It copies only while Run runs.
func NewAuditMirror(st AuditMirrorStore, logs LogWriter, log *slog.Logger) *AuditMirror {
	return &AuditMirror{store: st, logs: logs, log: log, every: 5 * time.Second, batchSize: 500}
}

// Run mirrors new audit events until ctx is done. It is meant to run as a
// [Worker].
func (m *AuditMirror) Run(ctx context.Context) {
	for {
		n, err := m.mirror(ctx)
		if err != nil && ctx.Err() == nil {
			m.log.Warn("mirror audit events", "err", err)
		}
		if err == nil && n == m.batchSize {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.every):
		}
	}
}

// mirror copies one batch of events after the cursor and advances the
// cursor past it once the store accepted it. It returns how many it copied.
func (m *AuditMirror) mirror(ctx context.Context) (int, error) {
	cursor, err := m.cursor(ctx)
	if err != nil {
		return 0, err
	}
	events, err := m.store.AuditEventsAfter(ctx, cursor, m.batchSize)
	if err != nil || len(events) == 0 {
		return 0, err
	}
	records := make([]victoria.Record, len(events))
	for i, e := range events {
		records[i] = auditRecord(e)
	}
	if err := m.logs.InsertJSONLines(ctx, records, victoria.InsertOptions{StreamFields: StreamFields}); err != nil {
		return 0, err
	}
	last := events[len(events)-1].ID
	if err := m.store.SetSetting(ctx, auditCursorKey, strconv.FormatInt(last, 10)); err != nil {
		return 0, err
	}
	return len(events), nil
}

func (m *AuditMirror) cursor(ctx context.Context) (int64, error) {
	v, err := m.store.Setting(ctx, auditCursorKey)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("observability: audit mirror cursor %q: %w", v, err)
	}
	return n, nil
}

// auditRecord returns the VictoriaLogs record of an audit event: its
// columns as fields, attributed to its service, or to shed.
func auditRecord(e store.AuditEvent) victoria.Record {
	r := victoria.Record{
		"_time":      e.Time.UTC().Format(time.RFC3339Nano),
		"_msg":       e.Action + " " + e.Outcome,
		"id":         strconv.FormatInt(e.ID, 10),
		"actor_kind": e.ActorKind,
		"actor":      e.Actor,
		"actor_ip":   e.ActorIP,
		"action":     e.Action,
		"outcome":    e.Outcome,
		"project_id": e.ProjectID,
		"service_id": e.ServiceID,
		"target":     e.Target,
		"detail":     e.Detail,
	}
	stampRecord(r, otlp.Source{
		ProjectID: e.ProjectID,
		ServiceID: cmp.Or(e.ServiceID, shedSource.ServiceID),
		Origin:    "audit",
	})
	return r
}
