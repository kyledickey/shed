package store

import (
	"context"
	"fmt"
	"time"
)

// InsertMetricSamples stores samples in one transaction, replacing any
// sample of the same service at the same second. Samples of services that do
// not exist, such as a container outliving its deleted service, are skipped.
func (s *Store) InsertMetricSamples(ctx context.Context, samples []MetricSample) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: insert metric samples: %w", err)
	}
	defer tx.Rollback()
	for _, m := range samples {
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO metric_samples
			(service_id, ts, cpu, memory, net_rx, net_tx, disk_read, disk_write)
			SELECT ?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8
			WHERE EXISTS (SELECT 1 FROM services WHERE id = ?1)`,
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

// InsertHostSample stores a host sample, replacing any at the same second.
func (s *Store) InsertHostSample(ctx context.Context, m HostSample) error {
	err := s.exec(ctx, `INSERT OR REPLACE INTO host_samples
		(ts, cpu, memory, disk_used, net_rx, net_tx, disk_read, disk_write)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Time.Unix(), m.CPU, m.Memory, m.DiskUsed, m.NetRx, m.NetTx, m.DiskRead, m.DiskWrite)
	if err != nil {
		return fmt.Errorf("store: insert host sample: %w", err)
	}
	return nil
}

// HostBuckets is like [Store.MetricBuckets] for host samples.
func (s *Store) HostBuckets(ctx context.Context, from time.Time, step time.Duration, n int) ([]HostBucket, error) {
	start, width := from.Unix(), int64(step/time.Second)
	if width <= 0 {
		return nil, fmt.Errorf("store: host buckets: step %v is under a second", step)
	}
	bs, err := queryAll(ctx, s, func(r scanner) (HostBucket, error) {
		var b HostBucket
		err := r.Scan(&b.Index, &b.CPU, &b.Memory, &b.DiskUsed, &b.NetRx, &b.NetTx, &b.DiskRead, &b.DiskWrite)
		return b, err
	}, `SELECT (ts - ?1) / ?2 AS bucket,
			AVG(cpu), AVG(memory), AVG(disk_used), AVG(net_rx), AVG(net_tx), AVG(disk_read), AVG(disk_write)
		FROM host_samples
		WHERE ts >= ?1 AND ts < ?1 + ?2 * ?3
		GROUP BY bucket ORDER BY bucket`, start, width, n)
	if err != nil {
		return nil, fmt.Errorf("store: host buckets: %w", err)
	}
	return bs, nil
}

// DeleteMetricSamplesBefore deletes every service and host metric sample
// older than t.
func (s *Store) DeleteMetricSamplesBefore(ctx context.Context, t time.Time) error {
	if err := s.exec(ctx, `DELETE FROM metric_samples WHERE ts < ?`, t.Unix()); err != nil {
		return fmt.Errorf("store: delete metric samples: %w", err)
	}
	if err := s.exec(ctx, `DELETE FROM host_samples WHERE ts < ?`, t.Unix()); err != nil {
		return fmt.Errorf("store: delete host samples: %w", err)
	}
	return nil
}
