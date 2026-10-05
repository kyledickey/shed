package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const fenceCols = `service_id, restore_id, phase, image, volume_ids, was_stopped, created_at`

func scanFence(r scanner) (RestoreFence, error) {
	var f RestoreFence
	var vols string
	err := r.Scan(&f.ServiceID, &f.RestoreID, &f.Phase, &f.Image, &vols, &f.WasStopped, (*timestamp)(&f.CreatedAt))
	f.VolumeIDs = strings.Fields(vols)
	return f, err
}

// inTx runs fn in a transaction, which it commits if fn succeeds.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateRestoreFence stores the fence f and marks its service stopped, in one
// transaction. It records whether the service was stopped before, sets the
// creation time if it is unset, and returns f with both filled in. It returns
// ErrNotFound for an unknown service and ErrConflict if the service already
// has a fence.
func (s *Store) CreateRestoreFence(ctx context.Context, f RestoreFence) (RestoreFence, error) {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now()
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT stopped FROM services WHERE id = ?`, f.ServiceID).Scan(&f.WasStopped)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO restore_fences (`+fenceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.ServiceID, f.RestoreID, f.Phase, f.Image, strings.Join(f.VolumeIDs, " "), f.WasStopped,
			formatTime(f.CreatedAt)); err != nil {
			return mapError(err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE services SET stopped = 1 WHERE id = ?`, f.ServiceID)
		return err
	})
	if err != nil {
		return RestoreFence{}, fmt.Errorf("store: fence service %s: %w", f.ServiceID, err)
	}
	return f, nil
}

// SetRestorePhase records the phase of a service's fence. It returns
// ErrNotFound if the service has no fence.
func (s *Store) SetRestorePhase(ctx context.Context, serviceID string, phase RestorePhase) error {
	if err := s.execOne(ctx, `UPDATE restore_fences SET phase = ? WHERE service_id = ?`, phase, serviceID); err != nil {
		return fmt.Errorf("store: set restore phase of service %s: %w", serviceID, err)
	}
	return nil
}

// LiftRestoreFence removes a service's fence and sets the service's stopped
// flag back to what it was before the fence, in one transaction. It returns
// ErrNotFound if the service has no fence.
func (s *Store) LiftRestoreFence(ctx context.Context, serviceID string) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var wasStopped bool
		err := tx.QueryRowContext(ctx, `SELECT was_stopped FROM restore_fences WHERE service_id = ?`, serviceID).Scan(&wasStopped)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE services SET stopped = ? WHERE id = ?`, wasStopped, serviceID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM restore_fences WHERE service_id = ?`, serviceID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: lift restore fence of service %s: %w", serviceID, err)
	}
	return nil
}

// DeleteRestoreFence removes a service's fence and leaves the service's
// stopped flag as it is. A missing fence is not an error.
func (s *Store) DeleteRestoreFence(ctx context.Context, serviceID string) error {
	if err := s.exec(ctx, `DELETE FROM restore_fences WHERE service_id = ?`, serviceID); err != nil {
		return fmt.Errorf("store: delete restore fence of service %s: %w", serviceID, err)
	}
	return nil
}

// RestoreFence returns the fence of a service. It returns ErrNotFound if the
// service has no fence.
func (s *Store) RestoreFence(ctx context.Context, serviceID string) (RestoreFence, error) {
	f, err := queryOne(ctx, s, scanFence, `SELECT `+fenceCols+` FROM restore_fences WHERE service_id = ?`, serviceID)
	if err != nil {
		return RestoreFence{}, fmt.Errorf("store: restore fence of service %s: %w", serviceID, err)
	}
	return f, nil
}

// RestoreFences returns every fence, oldest first.
func (s *Store) RestoreFences(ctx context.Context) ([]RestoreFence, error) {
	fs, err := queryAll(ctx, s, scanFence, `SELECT `+fenceCols+` FROM restore_fences ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list restore fences: %w", err)
	}
	return fs, nil
}
