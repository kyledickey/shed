package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

// pushRetryInterval is how often pushes that could not be deployed yet are
// tried again.
const pushRetryInterval = 10 * time.Second

// ReplayPushes deploys pending pushes: right away, which replays those a
// restart interrupted, and then every few seconds, which deploys those that
// arrived while their service was held or fenced once it no longer is. It
// returns when ctx is done. Start it after the deployer has reconciled.
func (s *Server) ReplayPushes(ctx context.Context) {
	t := time.NewTicker(s.pushRetry)
	defer t.Stop()
	for {
		pushes, err := s.store.PendingPushes(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Error("list pending pushes", "err", err)
		}
		for _, p := range pushes {
			if ctx.Err() != nil {
				return
			}
			s.deployPush(ctx, p.ServiceID)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// receivePush stores p as its service's pending push and tries to deploy it.
// It returns only the error of storing it: a push that cannot deploy now
// is kept for ReplayPushes. Storing it under s.pushMu means the deployment
// recorded as its predecessor is the one any earlier push or manual deploy
// created, not one about to be created.
func (s *Server) receivePush(ctx context.Context, p store.PendingPush) error {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	if _, err := s.store.SetPendingPush(ctx, p); err != nil {
		return err
	}
	s.deployPushLocked(ctx, p.ServiceID)
	return nil
}

// deployPush deploys a service's pending push and removes it, or keeps it to
// try again if the service cannot take a deployment now. A push is dropped
// instead if the service no longer deploys that branch on push, or if a
// deployment was created since the push arrived, which the push must not
// supersede.
func (s *Server) deployPush(ctx context.Context, serviceID string) {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	s.deployPushLocked(ctx, serviceID)
}

// deployPushLocked is deployPush with s.pushMu held. Every deployment the
// API creates is created under s.pushMu, so none can appear between the
// check that the push's predecessor is still the latest deployment and the
// push's own deployment.
func (s *Server) deployPushLocked(ctx context.Context, serviceID string) {
	p, err := s.store.PendingPush(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		s.pushFailed(serviceID, "", err)
		return
	}
	drop := func(reason string) {
		s.log.Info("pending push dropped", "service", serviceID, "sha", p.CommitSHA, "reason", reason)
		s.forgetPush(p)
	}
	svc, err := s.store.Service(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		s.forgetPush(p)
		return
	}
	if err != nil {
		s.pushFailed(serviceID, p.ID, err)
		return
	}
	if svc.Kind != "app" || !svc.AutoDeploy || svc.Branch != p.Branch || !strings.EqualFold(svc.Repo, p.Repo) {
		drop("the service no longer deploys this branch on push")
		return
	}
	latest, err := s.store.LatestDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		latest, err = store.Deployment{}, nil
	}
	if err != nil {
		s.pushFailed(serviceID, p.ID, err)
		return
	}
	if latest.ID != p.PriorDeploymentID {
		drop("a newer deployment exists")
		return
	}
	commit := deploy.Commit{SHA: p.CommitSHA, Message: p.CommitMessage, Author: p.CommitAuthor}
	if _, err := s.deployer.Deploy(ctx, serviceID, store.TriggerPush, commit); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.forgetPush(p)
			return
		}
		s.pushFailed(serviceID, p.ID, err)
		return
	}
	s.forgetPush(p)
}

// forgetPush removes a pending push that was deployed or dropped. If that
// fails, the next attempt finds the deployment it created, which is not the
// push's predecessor, and drops the push.
func (s *Server) forgetPush(p store.PendingPush) {
	delete(s.pushErrs, p.ServiceID)
	// The deployment exists now, so remove the push even if ctx ended.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.DeletePendingPush(ctx, p.ServiceID, p.ID); err != nil {
		s.log.Error("delete pending push", "service", p.ServiceID, "err", err)
	}
}

// pushFailed logs why a pending push was not deployed, once per push and
// reason, so that a long hold does not repeat it every retry. s.pushMu must
// be held.
func (s *Server) pushFailed(serviceID, pushID string, err error) {
	key := pushID + " " + err.Error()
	if s.pushErrs[serviceID] == key {
		return
	}
	s.pushErrs[serviceID] = key
	switch {
	case errors.Is(err, deploy.ErrServiceBusy), errors.Is(err, deploy.ErrFenced), errors.Is(err, deploy.ErrDeleting):
		s.log.Warn("push deferred until the service can be deployed", "service", serviceID, "reason", err)
	case errors.Is(err, deploy.ErrStopped), errors.Is(err, context.Canceled):
		// Shutting down: the push is kept for the next start.
	default:
		s.log.Error("deploy pending push", "service", serviceID, "err", err)
	}
}
