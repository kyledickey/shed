package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PendingPush is the newest push to the branch an app service tracks, kept
// until it is deployed. Each service has at most one; a newer push replaces
// it.
type PendingPush struct {
	// ID changes with every push stored for the service, so a push that was
	// replaced is not mistaken for its successor.
	ID            string
	ServiceID     string
	Repo          string // owner/name
	Branch        string
	CommitSHA     string
	CommitMessage string
	CommitAuthor  string
	ReceivedAt    time.Time
	// PriorDeploymentID is the ID of the service's latest deployment when the
	// push was stored, or empty if it had none. Any other latest deployment
	// was created after the push arrived.
	PriorDeploymentID string
}

const pushCols = `id, service_id, repo, branch, commit_sha, commit_message, commit_author, received_at,
	prior_deployment_id`

func scanPush(r scanner) (PendingPush, error) {
	var p PendingPush
	err := r.Scan(&p.ID, &p.ServiceID, &p.Repo, &p.Branch, &p.CommitSHA, &p.CommitMessage, &p.CommitAuthor,
		(*timestamp)(&p.ReceivedAt), &p.PriorDeploymentID)
	return p, err
}

// SetPendingPush stores p as its service's pending push, replacing any older
// one. It assigns a new ID, the receipt time, and the service's latest
// deployment, in the order LatestDeployment uses, and returns p with them
// filled in. It returns ErrNotFound for an unknown service.
func (s *Store) SetPendingPush(ctx context.Context, p PendingPush) (PendingPush, error) {
	p.ID, p.ReceivedAt = NewID(), now()
	// Selecting from services turns an unknown service into no row, rather
	// than a foreign key error.
	err := s.db.QueryRowContext(ctx, `INSERT INTO pending_pushes (`+pushCols+`)
		SELECT ?, id, ?, ?, ?, ?, ?, ?, COALESCE((SELECT d.id FROM deployments d WHERE d.service_id = services.id
			ORDER BY d.created_at DESC, d.rowid DESC LIMIT 1), '')
		FROM services WHERE id = ?
		ON CONFLICT (service_id) DO UPDATE SET id = excluded.id, repo = excluded.repo,
			branch = excluded.branch, commit_sha = excluded.commit_sha,
			commit_message = excluded.commit_message, commit_author = excluded.commit_author,
			received_at = excluded.received_at, prior_deployment_id = excluded.prior_deployment_id
		RETURNING prior_deployment_id`,
		p.ID, p.Repo, p.Branch, p.CommitSHA, p.CommitMessage, p.CommitAuthor, formatTime(p.ReceivedAt), p.ServiceID,
	).Scan(&p.PriorDeploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return PendingPush{}, fmt.Errorf("store: set pending push of service %s: %w", p.ServiceID, err)
	}
	return p, nil
}

// PendingPush returns a service's pending push. It returns ErrNotFound if the
// service has none.
func (s *Store) PendingPush(ctx context.Context, serviceID string) (PendingPush, error) {
	p, err := queryOne(ctx, s, scanPush, `SELECT `+pushCols+` FROM pending_pushes WHERE service_id = ?`, serviceID)
	if err != nil {
		return PendingPush{}, fmt.Errorf("store: pending push of service %s: %w", serviceID, err)
	}
	return p, nil
}

// PendingPushes returns every pending push, oldest first.
func (s *Store) PendingPushes(ctx context.Context) ([]PendingPush, error) {
	ps, err := queryAll(ctx, s, scanPush, `SELECT `+pushCols+` FROM pending_pushes ORDER BY received_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list pending pushes: %w", err)
	}
	return ps, nil
}

// DeletePendingPush removes a service's pending push if its ID is still id,
// so that a newer push stored meanwhile stays. A missing push is not an
// error.
func (s *Store) DeletePendingPush(ctx context.Context, serviceID, id string) error {
	if err := s.exec(ctx, `DELETE FROM pending_pushes WHERE service_id = ? AND id = ?`, serviceID, id); err != nil {
		return fmt.Errorf("store: delete pending push of service %s: %w", serviceID, err)
	}
	return nil
}
