package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AuditEvent is a row of the audit log.
type AuditEvent struct {
	ID        int64
	Time      time.Time
	ActorKind string
	Actor     string
	ActorIP   string
	Action    string
	Outcome   string
	ProjectID string
	ServiceID string
	Target    string
	// Detail is a JSON object.
	Detail string
}

// AuditFilter selects audit events. Zero fields select everything.
type AuditFilter struct {
	// Before selects events with a lower ID.
	Before int64
	// Actor matches the actor or the actor kind.
	Actor string
	// Action matches the action or, followed by a dot, its prefix: "service"
	// matches service.create but not services.
	Action    string
	ServiceID string
	Outcome   string
	// Since selects events at or after it.
	Since time.Time
	// Limit is the most events to return; zero means no limit.
	Limit int
}

const auditCols = `id, ts, actor_kind, actor, actor_ip, action, outcome, project_id, service_id, target, detail`

func scanAuditEvent(r scanner) (AuditEvent, error) {
	var e AuditEvent
	err := r.Scan(&e.ID, (*timestamp)(&e.Time), &e.ActorKind, &e.Actor, &e.ActorIP, &e.Action,
		&e.Outcome, &e.ProjectID, &e.ServiceID, &e.Target, &e.Detail)
	return e, err
}

// InsertAuditEvent appends e to the audit log, at e.Time or now if it is
// zero, and returns it with its ID and time.
func (s *Store) InsertAuditEvent(ctx context.Context, e AuditEvent) (AuditEvent, error) {
	if e.Time.IsZero() {
		e.Time = now()
	}
	if e.Detail == "" {
		e.Detail = "{}"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO audit_events
		(ts, actor_kind, actor, actor_ip, action, outcome, project_id, service_id, target, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(e.Time), e.ActorKind, e.Actor, e.ActorIP, e.Action, e.Outcome,
		e.ProjectID, e.ServiceID, e.Target, e.Detail)
	if err != nil {
		return AuditEvent{}, fmt.Errorf("store: insert audit event %s: %w", e.Action, err)
	}
	if e.ID, err = res.LastInsertId(); err != nil {
		return AuditEvent{}, fmt.Errorf("store: insert audit event %s: %w", e.Action, err)
	}
	e.Time = e.Time.UTC().Truncate(time.Millisecond)
	return e, nil
}

// CountAuditEvent adds one to detail.count of the newest event at or after
// since with the action, actor kind, actor IP, and outcome of e, and reports
// whether there was one.
func (s *Store) CountAuditEvent(ctx context.Context, e AuditEvent, since time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE audit_events
		SET detail = json_set(detail, '$.count', coalesce(json_extract(detail, '$.count'), 1) + 1)
		WHERE id = (SELECT id FROM audit_events
			WHERE ts >= ? AND action = ? AND actor_kind = ? AND actor_ip = ? AND outcome = ?
			ORDER BY id DESC LIMIT 1)`,
		formatTime(since), e.Action, e.ActorKind, e.ActorIP, e.Outcome)
	if err != nil {
		return false, fmt.Errorf("store: count audit event %s: %w", e.Action, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: count audit event %s: %w", e.Action, err)
	}
	return n > 0, nil
}

// AuditEvents returns the events f selects, newest first.
func (s *Store) AuditEvents(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	var where []string
	var args []any
	if f.Before > 0 {
		where, args = append(where, "id < ?"), append(args, f.Before)
	}
	if f.Actor != "" {
		where, args = append(where, "(actor = ? OR actor_kind = ?)"), append(args, f.Actor, f.Actor)
	}
	if f.Action != "" {
		where = append(where, "(action = ? OR substr(action, 1, ?) = ?)")
		args = append(args, f.Action, len(f.Action)+1, f.Action+".")
	}
	if f.ServiceID != "" {
		where, args = append(where, "service_id = ?"), append(args, f.ServiceID)
	}
	if f.Outcome != "" {
		where, args = append(where, "outcome = ?"), append(args, f.Outcome)
	}
	if !f.Since.IsZero() {
		where, args = append(where, "ts >= ?"), append(args, formatTime(f.Since))
	}
	query := `SELECT ` + auditCols + ` FROM audit_events`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id DESC"
	if f.Limit > 0 {
		query, args = query+" LIMIT ?", append(args, f.Limit)
	}
	events, err := queryAll(ctx, s, scanAuditEvent, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: audit events: %w", err)
	}
	return events, nil
}

// AuditEventsAfter returns at most limit events with an ID above after,
// oldest first.
func (s *Store) AuditEventsAfter(ctx context.Context, after int64, limit int) ([]AuditEvent, error) {
	events, err := queryAll(ctx, s, scanAuditEvent, `SELECT `+auditCols+` FROM audit_events
		WHERE id > ? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, fmt.Errorf("store: audit events after %d: %w", after, err)
	}
	return events, nil
}

// DeleteAuditEventsBefore deletes the audit events older than t.
func (s *Store) DeleteAuditEventsBefore(ctx context.Context, t time.Time) error {
	if err := s.exec(ctx, `DELETE FROM audit_events WHERE ts < ?`, formatTime(t)); err != nil {
		return fmt.Errorf("store: delete audit events: %w", err)
	}
	return nil
}
