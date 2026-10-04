package store

import (
	"context"
	"fmt"
	"strings"
)

const deploymentCols = `id, service_id, status, trigger, commit_sha, commit_message, commit_author,
	image, port, container_id, error, created_at, started_at, finished_at`

func scanDeployment(r scanner) (Deployment, error) {
	var d Deployment
	err := r.Scan(&d.ID, &d.ServiceID, &d.Status, &d.Trigger, &d.CommitSHA, &d.CommitMessage,
		&d.CommitAuthor, &d.Image, &d.Port, &d.ContainerID, &d.Error, (*timestamp)(&d.CreatedAt),
		nullTimestamp{&d.StartedAt}, nullTimestamp{&d.FinishedAt})
	return d, err
}

// updateDeployment overwrites the mutable fields of a deployment; its
// arguments come from updateArgs.
const updateDeployment = `UPDATE deployments SET status = ?, commit_sha = ?, commit_message = ?,
	commit_author = ?, image = ?, port = ?, container_id = ?, error = ?, started_at = ?,
	finished_at = ? WHERE id = ?`

func updateArgs(d Deployment) []any {
	return []any{d.Status, d.CommitSHA, d.CommitMessage, d.CommitAuthor, d.Image, d.Port,
		d.ContainerID, d.Error, optionalTime(d.StartedAt), optionalTime(d.FinishedAt), d.ID}
}

// CreateDeployment stores d under a new ID and creation time, which it
// returns along with the other fields.
func (s *Store) CreateDeployment(ctx context.Context, d Deployment) (Deployment, error) {
	d.ID = NewID()
	d.CreatedAt = now()
	err := s.exec(ctx, `INSERT INTO deployments (`+deploymentCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ServiceID, d.Status, d.Trigger, d.CommitSHA, d.CommitMessage, d.CommitAuthor,
		d.Image, d.Port, d.ContainerID, d.Error, formatTime(d.CreatedAt),
		optionalTime(d.StartedAt), optionalTime(d.FinishedAt))
	if err != nil {
		return Deployment{}, fmt.Errorf("store: create deployment for service %s: %w", d.ServiceID, err)
	}
	return d, nil
}

// Deployment returns the deployment with the given ID, or ErrNotFound.
func (s *Store) Deployment(ctx context.Context, id string) (Deployment, error) {
	d, err := queryOne(ctx, s, scanDeployment, `SELECT `+deploymentCols+` FROM deployments WHERE id = ?`, id)
	if err != nil {
		return Deployment{}, fmt.Errorf("store: deployment %s: %w", id, err)
	}
	return d, nil
}

// Deployments returns a service's deployments, newest first. A limit of zero
// or less returns all of them.
func (s *Store) Deployments(ctx context.Context, serviceID string, limit int) ([]Deployment, error) {
	if limit <= 0 {
		limit = -1 // SQLite: no limit.
	}
	ds, err := queryAll(ctx, s, scanDeployment, `SELECT `+deploymentCols+`
		FROM deployments WHERE service_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT ?`, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: deployments of service %s: %w", serviceID, err)
	}
	return ds, nil
}

// UpdateDeployment overwrites the mutable fields of the deployment d.ID: its
// status, commit details, image, port, container, error, and start and
// finish times. It returns ErrNotFound for an unknown deployment.
func (s *Store) UpdateDeployment(ctx context.Context, d Deployment) error {
	if err := s.execOne(ctx, updateDeployment, updateArgs(d)...); err != nil {
		return fmt.Errorf("store: update deployment %s: %w", d.ID, err)
	}
	return nil
}

// ActivateDeployment records d, with its status set to active, as the active
// deployment of its service. In the same transaction it marks the service's
// previous active deployment, if any, removed, so a service never has two
// active deployments. It returns ErrNotFound for an unknown deployment.
func (s *Store) ActivateDeployment(ctx context.Context, d Deployment) error {
	if err := s.activateDeployment(ctx, d); err != nil {
		return fmt.Errorf("store: activate deployment %s: %w", d.ID, err)
	}
	return nil
}

func (s *Store) activateDeployment(ctx context.Context, d Deployment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE deployments SET status = ?
		WHERE service_id = ? AND status = ? AND id <> ?`,
		StatusRemoved, d.ServiceID, StatusActive, d.ID); err != nil {
		return mapError(err)
	}
	d.Status = StatusActive
	res, err := tx.ExecContext(ctx, updateDeployment+` AND service_id = ?`,
		append(updateArgs(d), d.ServiceID)...)
	if err != nil {
		return mapError(err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// ActiveDeployment returns the active deployment of a service, or ErrNotFound
// if there is none.
func (s *Store) ActiveDeployment(ctx context.Context, serviceID string) (Deployment, error) {
	d, err := queryOne(ctx, s, scanDeployment, `SELECT `+deploymentCols+`
		FROM deployments WHERE service_id = ? AND status = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, serviceID, StatusActive)
	if err != nil {
		return Deployment{}, fmt.Errorf("store: active deployment of service %s: %w", serviceID, err)
	}
	return d, nil
}

// LatestDeployment returns the most recently created deployment of a service,
// or ErrNotFound if there is none.
func (s *Store) LatestDeployment(ctx context.Context, serviceID string) (Deployment, error) {
	d, err := queryOne(ctx, s, scanDeployment, `SELECT `+deploymentCols+`
		FROM deployments WHERE service_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, serviceID)
	if err != nil {
		return Deployment{}, fmt.Errorf("store: latest deployment of service %s: %w", serviceID, err)
	}
	return d, nil
}

// DeploymentsByStatus returns the deployments, across all services, whose
// status is any of statuses, oldest first.
func (s *Store) DeploymentsByStatus(ctx context.Context, statuses ...DeploymentStatus) ([]Deployment, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(statuses)), ", ")
	ds, err := queryAll(ctx, s, scanDeployment, `SELECT `+deploymentCols+`
		FROM deployments WHERE status IN (`+placeholders+`)
		ORDER BY created_at, rowid`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: deployments by status: %w", err)
	}
	return ds, nil
}

// PruneDeployments deletes a service's finished deployments beyond its newest
// keep deployments and returns the IDs it deleted. Active, crashed, and
// in-progress deployments are never deleted.
func (s *Store) PruneDeployments(ctx context.Context, serviceID string, keep int) ([]string, error) {
	ids, err := queryAll(ctx, s, func(r scanner) (string, error) {
		var id string
		return id, r.Scan(&id)
	}, `DELETE FROM deployments WHERE id IN (
			SELECT id FROM deployments WHERE service_id = ?
			ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET ?)
		AND status IN (?, ?, ?, ?) RETURNING id`,
		serviceID, max(keep, 0), StatusFailed, StatusRemoved, StatusCanceled, StatusSkipped)
	if err != nil {
		return nil, fmt.Errorf("store: prune deployments of service %s: %w", serviceID, err)
	}
	return ids, nil
}
