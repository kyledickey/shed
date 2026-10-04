package store

import (
	"context"
	"fmt"
	"time"
)

// InsertMetricSamples stores samples in one transaction, replacing any
// sample of the same service at the same second.
func (s *Store) InsertMetricSamples(ctx context.Context, samples []MetricSample) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: insert metric samples: %w", err)
	}
	defer tx.Rollback()
	for _, m := range samples {
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO metric_samples
			(service_id, ts, cpu, memory, net_rx, net_tx, disk_read, disk_write)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			m.ServiceID, m.Time.Unix(), m.CPU, m.Memory, m.NetRx, m.NetTx, m.DiskRead, m.DiskWrite)
		if err != nil {
			return fmt.Errorf("store: insert metric sample of service %s: %w", m.ServiceID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: insert metric samples: %w", err)
	}
	return nil
}

// MetricBuckets divides the window of n buckets of width step starting at
// from, and returns the averages of a service's samples in each bucket. Empty
// buckets are omitted; the rest are in index order. Times are used at second
// precision.
func (s *Store) MetricBuckets(ctx context.Context, serviceID string, from time.Time, step time.Duration, n int) ([]MetricBucket, error) {
	start, width := from.Unix(), int64(step/time.Second)
	if width <= 0 {
		return nil, fmt.Errorf("store: metric buckets: step %v is under a second", step)
	}
	bs, err := queryAll(ctx, s, func(r scanner) (MetricBucket, error) {
		var b MetricBucket
		err := r.Scan(&b.Index, &b.CPU, &b.Memory, &b.NetRx, &b.NetTx, &b.DiskRead, &b.DiskWrite)
		return b, err
	}, `SELECT (ts - ?1) / ?2 AS bucket,
			AVG(cpu), AVG(memory), AVG(net_rx), AVG(net_tx), AVG(disk_read), AVG(disk_write)
		FROM metric_samples
		WHERE service_id = ?3 AND ts >= ?1 AND ts < ?1 + ?2 * ?4
		GROUP BY bucket ORDER BY bucket`, start, width, serviceID, n)
	if err != nil {
		return nil, fmt.Errorf("store: metric buckets of service %s: %w", serviceID, err)
	}
	return bs, nil
}

// DeleteMetricSamplesBefore deletes every metric sample older than t.
func (s *Store) DeleteMetricSamplesBefore(ctx context.Context, t time.Time) error {
	if err := s.exec(ctx, `DELETE FROM metric_samples WHERE ts < ?`, t.Unix()); err != nil {
		return fmt.Errorf("store: delete metric samples: %w", err)
	}
	return nil
}
