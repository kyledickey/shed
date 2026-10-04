package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// StopService stops a service: deployments in progress are canceled, the
// active deployment's container is stopped but kept, and the service's
// domains stop routing. The active deployment stays active, so StartService
// or the next deployment brings the service back.
func (d *Deployer) StopService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	if _, err := d.store.Service(ctx, serviceID); err != nil {
		return err
	}
	// Halt first, so a deployment that is switching over cannot clear the
	// flag after it is set.
	d.halt(serviceID, errHalted)
	if err := d.store.SetServiceStopped(ctx, serviceID, true); err != nil {
		return err
	}
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return err
	case dep.ContainerID != "":
		err := d.docker.Stop(ctx, dep.ContainerID, d.stopTimeout)
		if err != nil && !docker.IsNotFound(err) {
			return fmt.Errorf("deploy: stop service %s: %w", serviceID, err)
		}
	}
	d.applyRoutesLogged(ctx)
	d.log.Info("service stopped", "service", serviceID)
	return nil
}

// StartService starts the active deployment's container of a stopped
// service, recreating it if it is gone, and routes its domains to it again.
// It returns ErrNoContainer if the service has no active deployment.
func (d *Deployer) StartService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNoContainer
	}
	if err != nil {
		return err
	}
	if err := d.store.SetServiceStopped(ctx, serviceID, false); err != nil {
		return err
	}
	if err := d.ensureRunning(ctx, dep); err != nil {
		if svc.Stopped {
			if err := d.store.SetServiceStopped(context.WithoutCancel(ctx), serviceID, true); err != nil {
				d.log.Error("restore stopped flag", "service", serviceID, "err", err)
			}
		}
		return fmt.Errorf("deploy: start service %s: %w", serviceID, err)
	}
	d.applyRoutesLogged(ctx)
	d.log.Info("service started", "service", serviceID)
	return nil
}

// RestartService restarts the active deployment's container of a service. It
// returns ErrServiceStopped if the service is stopped and ErrNoContainer if it
// has no active container.
func (d *Deployer) RestartService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	if svc.Stopped {
		return ErrServiceStopped
	}
	dep, err := d.store.ActiveDeployment(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && dep.ContainerID == "") {
		return ErrNoContainer
	}
	if err != nil {
		return err
	}
	if err := d.docker.Restart(ctx, dep.ContainerID, d.stopTimeout); err != nil {
		if docker.IsNotFound(err) {
			return ErrNoContainer
		}
		return fmt.Errorf("deploy: restart service %s: %w", serviceID, err)
	}
	// The container may come back with a different address.
	d.applyRoutesLogged(ctx)
	d.log.Info("service restarted", "service", serviceID)
	return nil
}
