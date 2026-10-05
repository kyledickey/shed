package deploy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/store"
)

// StopService stops a service: deployments in progress are canceled, the
// active deployment's container is stopped but kept, and the service's
// domains stop routing. The active deployment stays active, so StartService
// or the next deployment brings the service back. It returns ErrServiceBusy
// while the service is held.
func (d *Deployer) StopService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	if _, err := d.store.Service(ctx, serviceID); err != nil {
		return err
	}
	if err := d.checkHeld(serviceID); err != nil {
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
// It returns ErrNoContainer if the service has no active deployment,
// ErrServiceBusy while it is held, ErrFenced while it is fenced, and
// ErrImageUnavailable if the container is gone and so is its image.
func (d *Deployer) StartService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	if err := d.checkHeld(serviceID); err != nil {
		return err
	}
	if err := d.checkFence(ctx, serviceID); err != nil {
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
	if _, err := d.ensureRunning(ctx, dep); err != nil {
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
// returns ErrServiceStopped if the service is stopped, ErrNoContainer if it
// has no active container, ErrServiceBusy while it is held, and ErrFenced
// while it is fenced.
func (d *Deployer) RestartService(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	if err := d.checkHeld(serviceID); err != nil {
		return err
	}
	if err := d.checkFence(ctx, serviceID); err != nil {
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

// ClearRestoreFence removes the fence that a failed restore left on a
// service, keeping the data the service has now. The service stays stopped
// until it is started or deployed. A service without a fence is left as it
// is. It returns ErrServiceBusy while the service is held, such as during a
// restore.
func (d *Deployer) ClearRestoreFence(ctx context.Context, serviceID string) error {
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	if _, err := d.store.Service(ctx, serviceID); err != nil {
		return err
	}
	if err := d.checkHeld(serviceID); err != nil {
		return err
	}
	f, err := d.store.RestoreFence(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := d.store.DeleteRestoreFence(ctx, serviceID); err != nil {
		return err
	}
	d.log.Warn("restore fence cleared; keeping the service's current data",
		"service", serviceID, "restore", f.RestoreID, "phase", f.Phase)
	return nil
}

// checkFence returns ErrFenced if a failed restore left a service fenced.
func (d *Deployer) checkFence(ctx context.Context, serviceID string) error {
	_, err := d.store.RestoreFence(ctx, serviceID)
	switch {
	case err == nil:
		return ErrFenced
	case errors.Is(err, store.ErrNotFound):
		return nil
	default:
		return err
	}
}

// errReleased is returned by the methods of a Held after Release.
var errReleased = errors.New("deploy: hold was released")

// Hold takes exclusive control of a service for a backup or restore.
// Deployments in progress are canceled, and until Release, deploying,
// redeploying, starting, stopping, restarting, or deleting the service,
// deleting one of its volumes, and deleting its project return ErrServiceBusy.
// Holding a service that is already held returns ErrServiceBusy, and holding
// one that is being deleted returns ErrDeleting.
func (d *Deployer) Hold(ctx context.Context, serviceID string) (*Held, error) {
	// Check before waiting for controlMu, which a deletion keeps until it ends.
	if err := d.reserve(ctx, serviceID, false); err != nil {
		return nil, err
	}
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	if err := d.reserve(ctx, serviceID, true); err != nil {
		return nil, err
	}
	// New deployments are refused now. Wait for the current one to end,
	// including restarting a predecessor it stopped, so nothing else touches
	// the service's containers until Release.
	d.halt(serviceID, errHeld)
	d.log.Info("service held", "service", serviceID)
	return &Held{d: d, serviceID: serviceID}, nil
}

// reserve checks that a service can be held and, if mark is set, marks it
// held.
func (d *Deployer) reserve(ctx context.Context, serviceID string, mark bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return ErrStopped
	}
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return err
	}
	if err := d.admission(svc); err != nil {
		return err
	}
	if mark {
		d.held[svc.ID] = svc.ProjectID
	}
	return nil
}

// admission returns ErrDeleting or ErrServiceBusy if svc must not be deployed
// or held now. d.mu must be held.
func (d *Deployer) admission(svc store.Service) error {
	if d.deletingServices[svc.ID] || d.deletingProjects[svc.ProjectID] {
		return ErrDeleting
	}
	if _, ok := d.held[svc.ID]; ok {
		return ErrServiceBusy
	}
	return nil
}

// checkHeld returns ErrServiceBusy if a service is held.
func (d *Deployer) checkHeld(serviceID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.held[serviceID]; ok {
		return ErrServiceBusy
	}
	return nil
}

// Held is a service under Hold. It is safe for concurrent use.
type Held struct {
	d         *Deployer
	serviceID string

	mu       sync.Mutex
	released bool
}

// Active returns the service's active deployment, or store.ErrNotFound if it
// has none.
func (h *Held) Active(ctx context.Context) (store.Deployment, error) {
	return h.d.store.ActiveDeployment(ctx, h.serviceID)
}

// Running reports whether the active deployment's container is running.
func (h *Held) Running(ctx context.Context) (bool, error) {
	dep, err := h.Active(ctx)
	if errors.Is(err, store.ErrNotFound) || (err == nil && dep.ContainerID == "") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	c, err := h.d.docker.Inspect(ctx, dep.ContainerID)
	if docker.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("deploy: inspect service %s: %w", h.serviceID, err)
	}
	return c.Running, nil
}

// StopAndRemove stops and removes every container of the service, so its
// volumes can be removed and recreated. The active deployment stays active
// without a container, and its domains stop routing. Release starts it again.
func (h *Held) StopAndRemove(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return errReleased
	}
	d := h.d
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	containers, err := d.docker.List(ctx, map[string]string{labelService: h.serviceID})
	if err != nil {
		return fmt.Errorf("deploy: remove containers of %s: %w", h.serviceID, err)
	}
	for _, c := range containers {
		if c.Running {
			if err := d.docker.Stop(ctx, c.ID, d.stopTimeout); err != nil && !docker.IsNotFound(err) {
				return fmt.Errorf("deploy: stop container %s: %w", c.Name, err)
			}
		}
		if err := d.docker.Remove(ctx, c.ID); err != nil {
			return fmt.Errorf("deploy: remove container %s: %w", c.Name, err)
		}
	}
	dep, err := d.store.ActiveDeployment(ctx, h.serviceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return err
	case dep.ContainerID != "":
		dep.ContainerID = ""
		if err := d.store.UpdateDeployment(ctx, dep); err != nil {
			return err
		}
	}
	d.applyRoutesLogged(ctx)
	d.log.Info("held service removed its containers", "service", h.serviceID)
	return nil
}

// Release lifts the hold. Unless the user stopped the service, the active
// deployment's container is started first, recreated if it was removed; then
// routes are applied. The hold is lifted even if starting fails, and the
// error is returned. Release is not canceled with ctx, so a canceled restore
// still brings the service back. Calls after the first do nothing.
func (h *Held) Release(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return nil
	}
	h.released = true
	d := h.d
	ctx = context.WithoutCancel(ctx)
	d.controlMu.Lock()
	defer d.controlMu.Unlock()
	// Start while still held, so no deployment can run a second container
	// on the service's volumes meanwhile.
	err := h.start(ctx)
	d.applyRoutesLogged(ctx)
	d.mu.Lock()
	delete(d.held, h.serviceID)
	d.mu.Unlock()
	if err != nil {
		return fmt.Errorf("deploy: release service %s: %w", h.serviceID, err)
	}
	d.log.Info("service released", "service", h.serviceID)
	return nil
}

// start ensures the active deployment's container is running unless the
// service is stopped.
func (h *Held) start(ctx context.Context) error {
	svc, err := h.d.store.Service(ctx, h.serviceID)
	if err != nil {
		return err
	}
	if svc.Stopped {
		return nil
	}
	dep, err := h.d.store.ActiveDeployment(ctx, h.serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = h.d.ensureRunning(ctx, dep)
	return err
}
