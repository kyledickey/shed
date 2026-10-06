// Package control holds shed's operations on projects, services,
// deployments, variables, domains, volumes, backups, metrics, and updates:
// the rules every interface to shed shares. The HTTP API and the MCP server
// call a [Plane] instead of the store and the deployer directly, so that both
// validate the same way, serialize deployments under the same lock, and show
// the same state.
//
// Plane returns plain views (for example [ServiceView]) rather than wire
// types, and reports failures callers branch on as an [*Error] with a
// message fit to show, or as the sentinel errors of the packages below it;
// [Explain] turns either into a kind and a message.
package control

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/logtail"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/update"
)

// Store is the state Plane reads and changes. *store.Store implements it.
type Store interface {
	Projects(ctx context.Context) ([]store.Project, error)
	Project(ctx context.Context, id string) (store.Project, error)
	CreateProject(ctx context.Context, name string) (store.Project, error)
	RenameProject(ctx context.Context, id, name string) error

	Services(ctx context.Context, projectID string) ([]store.Service, error)
	AllServices(ctx context.Context) ([]store.Service, error)
	Service(ctx context.Context, id string) (store.Service, error)
	CreateService(ctx context.Context, sv store.Service) (store.Service, error)
	UpdateService(ctx context.Context, sv store.Service) error
	DeleteService(ctx context.Context, id string) error
	ServicesForPush(ctx context.Context, repo, branch string) ([]store.Service, error)

	Variables(ctx context.Context, serviceID string) (map[string]string, error)
	SetVariables(ctx context.Context, serviceID string, vars map[string]string) error

	Domains(ctx context.Context, serviceID string) ([]store.Domain, error)
	CreateDomain(ctx context.Context, serviceID, host string, generated bool) (store.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	Volumes(ctx context.Context, serviceID string) ([]store.Volume, error)
	CreateVolume(ctx context.Context, serviceID, mountPath string) (store.Volume, error)

	Deployment(ctx context.Context, id string) (store.Deployment, error)
	Deployments(ctx context.Context, serviceID string, limit int) ([]store.Deployment, error)
	LatestDeployment(ctx context.Context, serviceID string) (store.Deployment, error)

	RestoreFence(ctx context.Context, serviceID string) (store.RestoreFence, error)
	LatestRestore(ctx context.Context, serviceID string) (store.Restore, error)
	Backup(ctx context.Context, id string) (store.Backup, error)

	SetPendingPush(ctx context.Context, p store.PendingPush) (store.PendingPush, error)
	PendingPush(ctx context.Context, serviceID string) (store.PendingPush, error)
	PendingPushes(ctx context.Context) ([]store.PendingPush, error)
	DeletePendingPush(ctx context.Context, serviceID, id string) error
}

var _ Store = (*store.Store)(nil)

// Deployer runs deployments and owns the Docker side of services.
// *deploy.Deployer implements it.
type Deployer interface {
	Deploy(ctx context.Context, serviceID string, trigger store.Trigger, c deploy.Commit) (store.Deployment, error)
	Redeploy(ctx context.Context, deploymentID string) (store.Deployment, error)
	AvailableImages(ctx context.Context, serviceID string, images []string) (map[string]bool, error)
	Cancel(ctx context.Context, deploymentID string) (store.Deployment, error)
	ServiceStatuses(ctx context.Context, projectID string) (map[string]deploy.ServiceStatus, error)
	ApplyRoutes(ctx context.Context) error
	StopService(ctx context.Context, serviceID string) error
	StartService(ctx context.Context, serviceID string) error
	RestartService(ctx context.Context, serviceID string) error
	ClearRestoreFence(ctx context.Context, serviceID string) error
	DeleteService(ctx context.Context, serviceID string) error
	DeleteProject(ctx context.Context, projectID string) error
	DeleteVolume(ctx context.Context, volumeID string) error
	FollowLog(ctx context.Context, deploymentID string, line func(string), status func(store.DeploymentStatus)) error
	BuildLog(ctx context.Context, deploymentID string, n int) ([]string, error)
	RuntimeLogs(ctx context.Context, serviceID string, tail int, follow bool, w io.Writer) error
	ResolveVariables(ctx context.Context, serviceID string) (map[string]string, error)
}

var _ Deployer = (*deploy.Deployer)(nil)

// Backups runs backups and restores and holds the backup settings.
// *backup.Manager implements it.
type Backups interface {
	BackUp(ctx context.Context, serviceID string) (store.Backup, error)
	Backups(ctx context.Context, serviceID string, limit int) ([]store.Backup, error)
	Restore(ctx context.Context, backupID string) (store.Restore, error)
	Delete(ctx context.Context, backupID string) error
	Open(ctx context.Context, backupID string) (io.ReadCloser, error)
	PauseService(ctx context.Context, serviceID string) (resume func(), err error)
	ForgetService(ctx context.Context, serviceID string) error
	Policy(ctx context.Context, serviceID string) (backup.Policy, error)
	SetPolicy(ctx context.Context, serviceID string, in backup.PolicyInput) (backup.Policy, error)
	Settings(ctx context.Context) (backup.Settings, error)
	SetSettings(ctx context.Context, in backup.SettingsInput) (backup.Settings, error)
	TestS3(ctx context.Context, in backup.S3Input) error
	Identity(ctx context.Context) (string, error)
}

var _ Backups = (*backup.Manager)(nil)

// Metrics answers resource usage queries. *metrics.Collector implements it.
type Metrics interface {
	Query(ctx context.Context, serviceID string, r metrics.Range) (metrics.Series, error)
	QueryHost(ctx context.Context, r metrics.Range) (metrics.HostSeries, error)
}

var _ Metrics = (*metrics.Collector)(nil)

// Logs holds the tail of shed's own log. *logtail.Tail implements it.
type Logs interface {
	Lines(n int) []string
	Follow(ctx context.Context, emit func(line string))
}

var _ Logs = (*logtail.Tail)(nil)

// Updates checks for, downloads, and installs new versions of shed.
// *update.Updater implements it.
type Updates interface {
	Status(ctx context.Context) (update.Status, error)
	Check(ctx context.Context) (update.Status, error)
	Download(ctx context.Context) (update.Status, error)
	SetAutoDownload(ctx context.Context, on bool) (update.Status, error)
	Install(ctx context.Context) (update.Status, error)
}

var _ Updates = (*update.Updater)(nil)

// GitHub reads repositories through the GitHub App. *github.Client
// implements it.
type GitHub interface {
	Commit(ctx context.Context, fullName, ref string) (github.Commit, error)
	Repos(ctx context.Context) ([]github.Repo, error)
	Branches(ctx context.Context, fullName string) ([]string, error)
}

var _ GitHub = (*github.Client)(nil)

// Config configures a Plane.
type Config struct {
	Store    Store
	Deployer Deployer
	Backups  Backups
	Metrics  Metrics
	Logs     Logs
	Updates  Updates
	// GitHub returns the GitHub client, or false until the GitHub App is
	// configured.
	GitHub func() (GitHub, bool)
	// BaseURL is the public dashboard URL. Its host cannot be a service
	// domain.
	BaseURL string
	// BaseDomain is the parent of generated service domains; empty disables
	// them.
	BaseDomain string
	Log        *slog.Logger
}

// Plane carries out operations on shed's state. It is safe for concurrent
// use.
type Plane struct {
	store         Store
	deployer      Deployer
	backups       Backups
	metrics       Metrics
	logs          Logs
	updates       Updates
	github        func() (GitHub, bool)
	dashboardHost string
	baseDomain    string
	log           *slog.Logger

	routes *routeState

	pushRetry time.Duration
	pushMu    sync.Mutex        // serializes pushes with every other deployment Plane creates
	pushErrs  map[string]string // last logged failure by service ID; guarded by pushMu
}

// New returns a Plane.
func New(cfg Config) *Plane {
	p := &Plane{
		store:      cfg.Store,
		deployer:   cfg.Deployer,
		backups:    cfg.Backups,
		metrics:    cfg.Metrics,
		logs:       cfg.Logs,
		updates:    cfg.Updates,
		github:     cfg.GitHub,
		baseDomain: strings.ToLower(cfg.BaseDomain),
		log:        cfg.Log,
		routes:     newRouteState(),
		pushRetry:  pushRetryInterval,
		pushErrs:   make(map[string]string),
	}
	if u, err := url.Parse(cfg.BaseURL); err == nil {
		p.dashboardHost = strings.TrimSuffix(u.Hostname(), ".")
	}
	if p.github == nil {
		p.github = func() (GitHub, bool) { return nil, false }
	}
	return p
}

// requireGitHub returns the GitHub client, or a conflict until the GitHub App
// is configured.
func (p *Plane) requireGitHub() (GitHub, error) {
	if gh, ok := p.github(); ok {
		return gh, nil
	}
	return nil, errorf(ErrConflict, "GitHub App is not configured")
}
