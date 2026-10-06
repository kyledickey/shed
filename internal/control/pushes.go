package control

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

// ServicesForPush returns the app services that deploy a branch of a
// repository on push.
func (p *Plane) ServicesForPush(ctx context.Context, repo, branch string) ([]store.Service, error) {
	return p.store.ServicesForPush(ctx, repo, branch)
}

// ReplayPushes deploys pending pushes: right away, which replays those a
// restart interrupted, and then every few seconds, which deploys those that
// arrived while their service was held or fenced once it no longer is. It
// returns when ctx is done. Start it after the deployer has reconciled.
func (p *Plane) ReplayPushes(ctx context.Context) {
	t := time.NewTicker(p.pushRetry)
	defer t.Stop()
	for {
		pushes, err := p.store.PendingPushes(ctx)
		if err != nil && ctx.Err() == nil {
			p.log.Error("list pending pushes", "err", err)
		}
		for _, push := range pushes {
			if ctx.Err() != nil {
				return
			}
			p.DeployPush(ctx, push.ServiceID)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ReceivePush stores push as its service's pending push and tries to deploy
// it. It returns only the error of storing it: a push that cannot deploy now
// is kept for ReplayPushes. Storing it under pushMu means the deployment
// recorded as its predecessor is the one any earlier push or manual deploy
// created, not one about to be created.
func (p *Plane) ReceivePush(ctx context.Context, push store.PendingPush) error {
	p.pushMu.Lock()
	defer p.pushMu.Unlock()
	if _, err := p.store.SetPendingPush(ctx, push); err != nil {
		return err
	}
	p.deployPushLocked(ctx, push.ServiceID)
	return nil
}

// DeployPush deploys a service's pending push and removes it, or keeps it to
// try again if the service cannot take a deployment now. A push is dropped
// instead if the service no longer deploys that branch on push, or if a
// deployment was created since the push arrived, which the push must not
// supersede.
func (p *Plane) DeployPush(ctx context.Context, serviceID string) {
	p.pushMu.Lock()
	defer p.pushMu.Unlock()
	p.deployPushLocked(ctx, serviceID)
}

// deployPushLocked is DeployPush with pushMu held. Every deployment Plane
// creates is created under pushMu, so none can appear between the check
// that the push's predecessor is still the latest deployment and the push's
// own deployment.
func (p *Plane) deployPushLocked(ctx context.Context, serviceID string) {
	push, err := p.store.PendingPush(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return
	}
	if err != nil {
		p.pushFailed(serviceID, "", err)
		return
	}
	drop := func(reason string) {
		p.log.Info("pending push dropped", "service", serviceID, "sha", push.CommitSHA, "reason", reason)
		p.forgetPush(push)
	}
	svc, err := p.store.Service(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		p.forgetPush(push)
		return
	}
	if err != nil {
		p.pushFailed(serviceID, push.ID, err)
		return
	}
	if svc.Kind != "app" || !svc.AutoDeploy || svc.Branch != push.Branch || !strings.EqualFold(svc.Repo, push.Repo) {
		drop("the service no longer deploys this branch on push")
		return
	}
	latest, err := p.store.LatestDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		latest, err = store.Deployment{}, nil
	}
	if err != nil {
		p.pushFailed(serviceID, push.ID, err)
		return
	}
	if latest.ID != push.PriorDeploymentID {
		drop("a newer deployment exists")
		return
	}
	commit := deploy.Commit{SHA: push.CommitSHA, Message: push.CommitMessage, Author: push.CommitAuthor}
	if _, err := p.deployer.Deploy(ctx, serviceID, store.TriggerPush, commit); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			p.forgetPush(push)
			return
		}
		p.pushFailed(serviceID, push.ID, err)
		return
	}
	p.forgetPush(push)
}

// forgetPush removes a pending push that was deployed or dropped. If that
// fails, the next attempt finds the deployment it created, which is not the
// push's predecessor, and drops the push. pushMu must be held.
func (p *Plane) forgetPush(push store.PendingPush) {
	delete(p.pushErrs, push.ServiceID)
	// The deployment exists now, so remove the push even if ctx ended.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.store.DeletePendingPush(ctx, push.ServiceID, push.ID); err != nil {
		p.log.Error("delete pending push", "service", push.ServiceID, "err", err)
	}
}

// pushFailed logs why a pending push was not deployed, once per push and
// reason, so that a long hold does not repeat it every retry. pushMu must be
// held.
func (p *Plane) pushFailed(serviceID, pushID string, err error) {
	key := pushID + " " + err.Error()
	if p.pushErrs[serviceID] == key {
		return
	}
	p.pushErrs[serviceID] = key
	switch {
	case errors.Is(err, deploy.ErrServiceBusy), errors.Is(err, deploy.ErrFenced), errors.Is(err, deploy.ErrDeleting):
		p.log.Warn("push deferred until the service can be deployed", "service", serviceID, "reason", err)
	case errors.Is(err, deploy.ErrStopped), errors.Is(err, context.Canceled):
		// Shutting down: the push is kept for the next start.
	default:
		p.log.Error("deploy pending push", "service", serviceID, "err", err)
	}
}
