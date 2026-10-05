// Package deploy runs shed's deployment pipeline.
//
// A [Deployer] turns deployments into running containers: it waits for CI,
// builds or pulls the image, resolves variables, starts the container, checks
// its health, switches the proxy routes, and retires the previous container.
// Each service has its own queue, and a newer deployment cancels older ones
// that are still in progress. The Deployer also reconciles state on boot,
// stops, starts, and restarts services, tears services and projects down, and
// derives service status.
//
// Docker, the image builder, the proxy, and GitHub are reached through small
// interfaces so they can be replaced, for example by fakes in tests.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/docker"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/proxy"
	"github.com/kyledickey/shed/internal/store"
)

// Docker runs containers. *docker.Client implements it.
type Docker interface {
	EnsureNetwork(ctx context.Context, name string) error
	RemoveNetwork(ctx context.Context, name string) error
	ReconnectNetwork(ctx context.Context, name, id string, aliases []string) error
	DisconnectNetwork(ctx context.Context, name, id string) error
	EnsureVolume(ctx context.Context, name string) error
	RemoveVolume(ctx context.Context, name string) error
	PullImage(ctx context.Context, ref string, w io.Writer) error
	ResolveImage(ctx context.Context, ref string) (string, error)
	ListImages(ctx context.Context, repo string) ([]docker.Image, error)
	ExposedPorts(ctx context.Context, ref string) ([]int, error)
	RemoveImage(ctx context.Context, ref string) error
	Run(ctx context.Context, spec docker.RunSpec) (string, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string, timeout time.Duration) error
	Restart(ctx context.Context, id string, timeout time.Duration) error
	Remove(ctx context.Context, id string) error
	Inspect(ctx context.Context, id string) (docker.Container, error)
	List(ctx context.Context, labels map[string]string) ([]docker.Container, error)
	Logs(ctx context.Context, id string, tail int, follow bool, w io.Writer) error
}

// Builder builds images from source. *build.Builder implements it.
type Builder interface {
	Build(ctx context.Context, id string, req build.Request, out io.Writer) error
}

// Proxy routes public traffic. *proxy.Proxy implements it.
type Proxy interface {
	Apply(routes []proxy.Route) error
}

// GitHub provides repository access and CI status. *github.Client implements
// it.
type GitHub interface {
	CloneURL(ctx context.Context, repo string) (string, error)
	CIStatus(ctx context.Context, repo, sha string) (github.CIState, error)
}

// Config configures a Deployer.
type Config struct {
	Store   *store.Store
	Docker  Docker
	Builder Builder
	// Proxy receives the routes after every change. Nil disables routing.
	Proxy Proxy
	// GitHub returns the GitHub client, or false while the GitHub App is not
	// configured.
	GitHub func() (GitHub, bool)
	// LogDir holds the build logs, one <deploymentID>.log per deployment.
	LogDir string
	// LogLimit caps each build log, in bytes. Output beyond it is replaced
	// by a truncation notice. Zero means unlimited.
	LogLimit int64
	// KeepDeployments is how many deployments of each service are kept after
	// a deployment ends. Older finished ones are deleted along with their
	// build logs. Zero keeps them all.
	KeepDeployments int
	// Dashboard, if its Host is set, is included in every route set.
	Dashboard proxy.Route
	Log       *slog.Logger
}

// Commit identifies the source revision of a deployment.
type Commit struct {
	SHA     string
	Message string
	Author  string
}

var (
	// ErrDeleting is returned while a service or its project is being deleted.
	ErrDeleting = errors.New("deploy: service is being deleted")
	// ErrStopped is returned for deployments requested after Stop.
	ErrStopped = errors.New("deploy: deployer is stopped")
	// ErrNotInProgress is returned when canceling a finished deployment.
	ErrNotInProgress = errors.New("deploy: deployment is not in progress")
	// ErrNoImage is returned when redeploying a deployment that has no image.
	ErrNoImage = errors.New("deploy: deployment has no image to redeploy")
	// ErrNoContainer is returned for runtime logs of a service that has no
	// active container, and when starting or restarting such a service.
	ErrNoContainer = errors.New("deploy: service has no active container")
	// ErrServiceStopped is returned when restarting a stopped service.
	ErrServiceStopped = errors.New("deploy: service is stopped")
	// ErrServiceBusy is returned while a service is held for a backup or
	// restore.
	ErrServiceBusy = errors.New("deploy: service is busy with a backup or restore")
	// ErrFenced is returned when deploying, redeploying, starting, or
	// restarting a service that a failed restore left fenced, because its
	// data may be partial. ClearRestoreFence lifts the refusal.
	ErrFenced = errors.New("deploy: service is fenced by a failed restore")
)

// Cancellation causes, which decide how an interrupted deployment is
// recorded.
var (
	errCanceled   = errors.New("canceled")
	errSuperseded = errors.New("superseded by a newer deployment")
	errDeleted    = errors.New("service deleted")
	errHalted     = errors.New("service stopped")
	errHeld       = errors.New("service held for a backup or restore")
	errShutdown   = errors.New("interrupted by shutdown")
)

// Deployer runs deployments. It is safe for concurrent use.
type Deployer struct {
	store     *store.Store
	docker    Docker
	builder   Builder
	proxy     Proxy
	github    func() (GitHub, bool)
	logDir    string
	logLimit  int64
	keep      int // deployments kept per service; 0 keeps all
	dashboard proxy.Route
	log       *slog.Logger

	// Timings and the health probe, replaced in tests.
	ciInterval     time.Duration
	ciTimeout      time.Duration
	healthInterval time.Duration
	healthTimeout  time.Duration
	healthReport   time.Duration // how often to report a pending health check
	recheckTimeout time.Duration // how long the health check may take again after promotion
	startupWatch   time.Duration // how long to watch a service without a port
	logDrain       time.Duration // how long to wait for an exited container's output
	stopTimeout    time.Duration
	logPoll        time.Duration
	probe          func(ctx context.Context, addr, path string) error

	ctx      context.Context // parent of every job; canceled by Stop
	shutdown context.CancelCauseFunc
	wg       sync.WaitGroup
	routesMu sync.Mutex // serializes computing and applying routes
	// controlMu serializes stopping, starting, restarting, holding, and
	// deleting services. It is taken before mu, never while holding it.
	controlMu sync.Mutex

	mu               sync.Mutex
	stopped          bool
	workers          map[string]*worker // by service ID
	deletingServices map[string]bool
	deletingProjects map[string]bool
	held             map[string]string // project ID by held service ID; changes under controlMu
}

// worker runs the deployments of one service, one at a time.
type worker struct {
	pending *store.Deployment       // next to run, or nil
	running string                  // ID of the running deployment, or ""
	cancel  context.CancelCauseFunc // cancels the running deployment
	ran     chan struct{}           // closed when the running deployment ends
	done    chan struct{}           // closed when the worker exits
}

// New returns a Deployer. Call Reconcile once before serving requests and
// Stop on shutdown.
func New(cfg Config) *Deployer {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Deployer{
		store:            cfg.Store,
		docker:           cfg.Docker,
		builder:          cfg.Builder,
		proxy:            cfg.Proxy,
		github:           cfg.GitHub,
		logDir:           cfg.LogDir,
		logLimit:         cfg.LogLimit,
		keep:             cfg.KeepDeployments,
		dashboard:        cfg.Dashboard,
		log:              cfg.Log,
		ciInterval:       10 * time.Second,
		ciTimeout:        60 * time.Minute,
		healthInterval:   time.Second,
		healthTimeout:    120 * time.Second,
		healthReport:     5 * time.Second,
		recheckTimeout:   10 * time.Second,
		startupWatch:     3 * time.Second,
		logDrain:         2 * time.Second,
		stopTimeout:      30 * time.Second,
		logPoll:          500 * time.Millisecond,
		probe:            probe,
		ctx:              ctx,
		shutdown:         cancel,
		workers:          make(map[string]*worker),
		deletingServices: make(map[string]bool),
		deletingProjects: make(map[string]bool),
		held:             make(map[string]string),
	}
}

// Stop cancels running deployments and waits for the workers to exit.
// Deployments still queued stay queued; Reconcile fails them on the next boot.
func (d *Deployer) Stop() {
	d.mu.Lock()
	d.stopped = true
	d.mu.Unlock()
	d.shutdown(errShutdown)
	d.wg.Wait()
}

// Deploy queues a new deployment of a service and returns it. Older
// deployments of the service that are still in progress are canceled. It
// returns ErrFenced while the service is fenced by a failed restore.
func (d *Deployer) Deploy(ctx context.Context, serviceID string, trigger store.Trigger, c Commit) (store.Deployment, error) {
	return d.enqueue(ctx, store.Deployment{
		ServiceID:     serviceID,
		Trigger:       trigger,
		CommitSHA:     c.SHA,
		CommitMessage: c.Message,
		CommitAuthor:  c.Author,
	})
}

// Redeploy queues a new deployment that runs the image of an earlier one,
// skipping CI and the build. It is how rollbacks work. It returns ErrNoImage
// if the deployment has no image to reuse, and ErrImageUnavailable if its
// image is no longer on this host.
func (d *Deployer) Redeploy(ctx context.Context, deploymentID string) (store.Deployment, error) {
	old, err := d.store.Deployment(ctx, deploymentID)
	if err != nil {
		return store.Deployment{}, err
	}
	if old.Image == "" || (!strings.HasPrefix(old.Image, "sha256:") && !strings.Contains(old.Image, "@sha256:") && !strings.HasPrefix(old.Image, imageRepo(old.ServiceID)+":")) {
		return store.Deployment{}, ErrNoImage
	}
	// Refuse now rather than after the pipeline has stopped the running
	// container of a service that cannot run two at once.
	if err := d.checkImage(ctx, old.Image); err != nil {
		return store.Deployment{}, err
	}
	return d.enqueue(ctx, store.Deployment{
		ServiceID:     old.ServiceID,
		Trigger:       store.TriggerRedeploy,
		CommitSHA:     old.CommitSHA,
		CommitMessage: old.CommitMessage,
		CommitAuthor:  old.CommitAuthor,
		Image:         old.Image,
	})
}

// enqueue stores dep as queued and hands it to its service's worker.
func (d *Deployer) enqueue(ctx context.Context, dep store.Deployment) (store.Deployment, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return store.Deployment{}, ErrStopped
	}
	svc, err := d.store.Service(ctx, dep.ServiceID)
	if err != nil {
		return store.Deployment{}, err
	}
	if err := d.admission(svc); err != nil {
		return store.Deployment{}, err
	}
	if err := d.checkFence(ctx, svc.ID); err != nil {
		return store.Deployment{}, err
	}
	dep.Status = store.StatusQueued
	dep, err = d.store.CreateDeployment(ctx, dep)
	if err != nil {
		return store.Deployment{}, fmt.Errorf("deploy: %w", err)
	}

	w := d.workers[dep.ServiceID]
	if w == nil {
		w = &worker{done: make(chan struct{})}
		d.workers[dep.ServiceID] = w
		d.wg.Add(1)
		go d.work(dep.ServiceID, w)
	}
	if w.pending != nil {
		d.abandon(*w.pending, store.StatusCanceled, errSuperseded.Error())
	}
	if w.cancel != nil {
		w.cancel(errSuperseded)
	}
	w.pending = &dep
	return dep, nil
}

// work runs a service's deployments until none are pending.
func (d *Deployer) work(serviceID string, w *worker) {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		dep := w.pending
		if dep == nil || d.stopped {
			delete(d.workers, serviceID)
			close(w.done)
			d.mu.Unlock()
			return
		}
		ctx, cancel := context.WithCancelCause(d.ctx)
		w.pending, w.running, w.cancel, w.ran = nil, dep.ID, cancel, make(chan struct{})
		d.mu.Unlock()

		d.run(ctx, *dep)
		cancel(nil)
		d.pruneHistory(serviceID)

		d.mu.Lock()
		close(w.ran)
		w.running, w.cancel = "", nil
		d.mu.Unlock()
	}
}

// Cancel cancels a deployment that is in progress and returns it. It returns
// ErrNotInProgress if the deployment has already finished.
func (d *Deployer) Cancel(ctx context.Context, deploymentID string) (store.Deployment, error) {
	// Read under the lock so the status cannot change before we act on it.
	d.mu.Lock()
	dep, err := d.store.Deployment(ctx, deploymentID)
	if err != nil {
		d.mu.Unlock()
		return store.Deployment{}, err
	}
	w := d.workers[dep.ServiceID]
	switch {
	case dep.Status.Terminal():
		d.mu.Unlock()
		return dep, ErrNotInProgress
	case w != nil && w.running == dep.ID:
		w.cancel(errCanceled)
		ran := w.ran
		d.mu.Unlock()
		select { // Let the pipeline record the cancellation.
		case <-ran:
		case <-ctx.Done():
		}
	default: // Queued, or left over from a previous run.
		if w != nil && w.pending != nil && w.pending.ID == dep.ID {
			w.pending = nil
		}
		d.abandon(dep, store.StatusCanceled, errCanceled.Error())
		d.mu.Unlock()
	}
	return d.store.Deployment(ctx, deploymentID)
}

// halt cancels everything queued or running for a service with cause and
// waits for its worker to exit.
func (d *Deployer) halt(serviceID string, cause error) {
	d.mu.Lock()
	w := d.workers[serviceID]
	if w == nil {
		d.mu.Unlock()
		return
	}
	if w.pending != nil {
		d.abandon(*w.pending, store.StatusCanceled, cause.Error())
		w.pending = nil
	}
	if w.cancel != nil {
		w.cancel(cause)
	}
	d.mu.Unlock()
	<-w.done
}

// abandon records that a deployment that is not running ended with status.
func (d *Deployer) abandon(dep store.Deployment, status store.DeploymentStatus, msg string) {
	now := time.Now()
	dep.Status, dep.Error, dep.FinishedAt = status, msg, &now
	if err := d.store.UpdateDeployment(context.Background(), dep); err != nil {
		d.log.Error("record deployment status", "deployment", dep.ID, "err", err)
	}
}

// logPath returns the build log file of a deployment.
func (d *Deployer) logPath(deploymentID string) string {
	return filepath.Join(d.logDir, deploymentID+".log")
}

// Docker object naming and labels.
const (
	labelProject    = "shed.project"
	labelService    = "shed.service"
	labelDeployment = "shed.deployment"
)

func networkName(projectID string) string { return "shed-" + projectID }
func volumeName(volumeID string) string   { return "shed-vol-" + volumeID }
func imageRepo(serviceID string) string   { return "shed/" + serviceID }
func containerName(serviceID, deploymentID string) string {
	return "shed-" + serviceID + "-" + deploymentID
}
