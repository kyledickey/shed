package store

import (
	"context"
	"fmt"
	"strings"
)

const deploymentCols = `id, service_id, status, trigger, commit_sha, commit_message, commit_author,
	image, container_id, error, created_at, started_at, finished_at`

func scanDeployment(r scanner) (Deployment, error) {
	var d Deployment
	err := r.Scan(&d.ID, &d.ServiceID, &d.Status, &d.Trigger, &d.CommitSHA, &d.CommitMessage,
		&d.CommitAuthor, &d.Image, &d.ContainerID, &d.Error, (*timestamp)(&d.CreatedAt),
		nullTimestamp{&d.StartedAt}, nullTimestamp{&d.FinishedAt})
	return d, err
}

// CreateDeployment stores d under a new ID and creation time, which it
// returns along with the other fields.
func (s *Store) CreateDeployment(ctx context.Context, d Deployment) (Deployment, error) {
	d.ID = NewID()
	d.CreatedAt = now()
	err := s.exec(ctx, `INSERT INTO deployments (`+deploymentCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.ServiceID, d.Status, d.Trigger, d.CommitSHA, d.CommitMessage, d.CommitAuthor,
		d.Image, d.ContainerID, d.Error, formatTime(d.CreatedAt),
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
// status, commit details, image, container, error, and start and finish
// times. It returns ErrNotFound for an unknown deployment.
func (s *Store) UpdateDeployment(ctx context.Context, d Deployment) error {
	err := s.execOne(ctx, `UPDATE deployments SET status = ?, commit_sha = ?, commit_message = ?,
		commit_author = ?, image = ?, container_id = ?, error = ?, started_at = ?, finished_at = ?
		WHERE id = ?`,
		d.Status, d.CommitSHA, d.CommitMessage, d.CommitAuthor, d.Image, d.ContainerID, d.Error,
		optionalTime(d.StartedAt), optionalTime(d.FinishedAt), d.ID)
	if err != nil {
		return fmt.Errorf("store: update deployment %s: %w", d.ID, err)
	}
	return nil
}

// ActiveDeployment returns the most recent active deployment of a service, or
// ErrNotFound if there is none.
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
