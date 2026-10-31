package store

import (
	"context"
	"fmt"
	"time"
)

// TelemetryCaps are a service's own ingest limits, overriding the defaults
// in the telemetry settings.
type TelemetryCaps struct {
	SpansPerSecond int
	LogsPerSecond  int
	Series         int
}

// TelemetryCaps returns a service's caps override. It returns ErrNotFound if
// the service has none.
func (s *Store) TelemetryCaps(ctx context.Context, serviceID string) (TelemetryCaps, error) {
	c, err := queryOne(ctx, s, scanCaps,
		`SELECT spans_per_second, logs_per_second, series FROM telemetry_caps WHERE service_id = ?`, serviceID)
	if err != nil {
		return TelemetryCaps{}, fmt.Errorf("store: telemetry caps of service %s: %w", serviceID, err)
	}
	return c, nil
}

// AllTelemetryCaps returns every caps override, keyed by service ID.
func (s *Store) AllTelemetryCaps(ctx context.Context) (map[string]TelemetryCaps, error) {
	type row struct {
		id   string
		caps TelemetryCaps
	}
	rows, err := queryAll(ctx, s, func(r scanner) (row, error) {
		var v row
		err := r.Scan(&v.id, &v.caps.SpansPerSecond, &v.caps.LogsPerSecond, &v.caps.Series)
		return v, err
	}, `SELECT service_id, spans_per_second, logs_per_second, series FROM telemetry_caps`)
	if err != nil {
		return nil, fmt.Errorf("store: telemetry caps: %w", err)
	}
	out := make(map[string]TelemetryCaps, len(rows))
	for _, r := range rows {
		out[r.id] = r.caps
	}
	return out, nil
}

// SetTelemetryCaps creates or replaces a service's caps override.
func (s *Store) SetTelemetryCaps(ctx context.Context, serviceID string, c TelemetryCaps) error {
	err := s.exec(ctx, `INSERT INTO telemetry_caps (service_id, spans_per_second, logs_per_second, series)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (service_id) DO UPDATE SET spans_per_second = excluded.spans_per_second,
			logs_per_second = excluded.logs_per_second, series = excluded.series`,
		serviceID, c.SpansPerSecond, c.LogsPerSecond, c.Series)
	if err != nil {
		return fmt.Errorf("store: set telemetry caps of service %s: %w", serviceID, err)
	}
	return nil
}

// DeleteTelemetryCaps removes a service's caps override, if it has one.
func (s *Store) DeleteTelemetryCaps(ctx context.Context, serviceID string) error {
	if err := s.exec(ctx, `DELETE FROM telemetry_caps WHERE service_id = ?`, serviceID); err != nil {
		return fmt.Errorf("store: delete telemetry caps of service %s: %w", serviceID, err)
	}
	return nil
}

func scanCaps(r scanner) (TelemetryCaps, error) {
	var c TelemetryCaps
	err := r.Scan(&c.SpansPerSecond, &c.LogsPerSecond, &c.Series)
	return c, err
}

// LogCursors returns the Docker timestamp of the last acknowledged line of
// every container whose output is shipped, keyed by container ID.
func (s *Store) LogCursors(ctx context.Context) (map[string]time.Time, error) {
	type row struct {
		id string
		ts string
	}
	rows, err := queryAll(ctx, s, func(r scanner) (row, error) {
		var v row
		err := r.Scan(&v.id, &v.ts)
		return v, err
	}, `SELECT container_id, ts FROM log_cursors`)
	if err != nil {
		return nil, fmt.Errorf("store: log cursors: %w", err)
	}
	out := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		t, err := time.Parse(time.RFC3339Nano, r.ts)
		if err != nil {
			return nil, fmt.Errorf("store: log cursor of %s: %w", r.id, err)
		}
		out[r.id] = t
	}
	return out, nil
}

// SetLogCursor records ts, at full precision, as the last acknowledged line
// of a container's output.
func (s *Store) SetLogCursor(ctx context.Context, containerID string, ts time.Time) error {
	err := s.exec(ctx, `INSERT INTO log_cursors (container_id, ts) VALUES (?, ?)
		ON CONFLICT (container_id) DO UPDATE SET ts = excluded.ts`,
		containerID, ts.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: set log cursor of %s: %w", containerID, err)
	}
	return nil
}

// DeleteLogCursor removes a container's log cursor, if it has one.
func (s *Store) DeleteLogCursor(ctx context.Context, containerID string) error {
	if err := s.exec(ctx, `DELETE FROM log_cursors WHERE container_id = ?`, containerID); err != nil {
		return fmt.Errorf("store: delete log cursor of %s: %w", containerID, err)
	}
	return nil
}

// DeleteLogCursors removes every log cursor.
func (s *Store) DeleteLogCursors(ctx context.Context) error {
	if err := s.exec(ctx, `DELETE FROM log_cursors`); err != nil {
		return fmt.Errorf("store: delete log cursors: %w", err)
	}
	return nil
}
