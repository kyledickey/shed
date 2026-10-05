// Package api serves shed's HTTP interface: the JSON API under /api, the
// server-sent event log streams, the GitHub App setup flow and webhook, and
// the dashboard single-page app.
//
// Handlers are thin: they validate input, call the store or the deployer, and
// map the result to the JSON types of the design document.
package api

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kyledickey/shed/internal/auth"
	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/store"
)

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
	RuntimeLogs(ctx context.Context, serviceID string, tail int, w io.Writer) error
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

// Config configures a Server.
type Config struct {
	Store    *store.Store
	Deployer Deployer
	Backups  Backups
	Metrics  Metrics
	// Logs is shed's own log, streamed to the dashboard.
	Logs Logs
	Auth *auth.Auth
	// GitHub holds the GitHub client. New fills it from the stored App
	// credentials, and the setup flow fills it once the App is created.
	GitHub *GitHubHolder
	// BaseURL is the public dashboard URL.
	BaseURL string
	// BaseDomain is the parent of generated service domains; empty disables
	// them.
	BaseDomain string
	// Web holds the built dashboard, with index.html at its root.
	Web fs.FS
	// HTTPClient is used for GitHub API calls. Nil means a default client.
	HTTPClient *http.Client
	Log        *slog.Logger
}

// Server is the HTTP interface.
type Server struct {
	store      *store.Store
	deployer   Deployer
	backups    Backups
	metrics    Metrics
	logs       Logs
	auth       *auth.Auth
	github     *GitHubHolder
	baseURL    string
	baseDomain string
	web        fs.FS
	httpClient *http.Client
	log        *slog.Logger

	webhooks   webhookGuard
	deliveries deliveryCache
	setupMu    sync.Mutex // serializes completing the GitHub setup
	tokenMu    sync.Mutex
	setupToken string // guarded by tokenMu; empty once GitHub is configured

	routes *routeState
}

// New returns a Server. It loads the GitHub App from the store, or, if none is
// configured, generates the one-time setup token and logs it.
func New(ctx context.Context, cfg Config) (*Server, error) {
	s := &Server{
		store:      cfg.Store,
		deployer:   cfg.Deployer,
		backups:    cfg.Backups,
		metrics:    cfg.Metrics,
		logs:       cfg.Logs,
		auth:       cfg.Auth,
		github:     cfg.GitHub,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		baseDomain: strings.ToLower(cfg.BaseDomain),
		web:        cfg.Web,
		httpClient: cfg.HTTPClient,
		log:        cfg.Log,
		routes:     newRouteState(),
	}
	if err := s.loadGitHub(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public routes.
	mux.HandleFunc("GET /api/auth/login", s.auth.Login)
	mux.HandleFunc("GET /api/auth/callback", s.auth.Callback)
	mux.Handle("POST /api/auth/logout", protectMutations(s.baseURL, requireJSON(http.HandlerFunc(s.auth.Logout))))
	mux.Handle("GET /api/setup", s.handle(s.getSetup))
	mux.Handle("GET /api/setup/github", s.handle(s.setupGitHub))
	mux.Handle("GET /api/setup/github/callback", s.handle(s.setupCallback))
	mux.Handle("POST /api/setup/github/import", protectMutations(s.baseURL, requireJSON(s.handle(s.importApp))))
	mux.Handle("POST /api/github/webhook", s.handle(s.webhook))

	// Routes that need a session.
	authed := func(pattern string, h handlerFunc) {
		mux.Handle(pattern, s.auth.Require(protectMutations(s.baseURL, requireJSON(s.handle(h)))))
	}
	authed("GET /api/me", s.me)

	authed("GET /api/projects", s.listProjects)
	authed("POST /api/projects", s.createProject)
	authed("GET /api/projects/{id}", s.getProject)
	authed("PATCH /api/projects/{id}", s.renameProject)
	authed("DELETE /api/projects/{id}", s.deleteProject)

	authed("POST /api/projects/{id}/services", s.createService)
	authed("GET /api/services/{id}", s.getService)
	authed("PATCH /api/services/{id}", s.patchService)
	authed("DELETE /api/services/{id}", s.deleteService)
	authed("POST /api/services/{id}/stop", s.controlService(Deployer.StopService))
	authed("POST /api/services/{id}/start", s.controlService(Deployer.StartService))
	authed("POST /api/services/{id}/restart", s.controlService(Deployer.RestartService))
	authed("POST /api/services/{id}/restore-fence/clear", s.controlService(Deployer.ClearRestoreFence))

	authed("GET /api/services/{id}/variables", s.getVariables)
	authed("PUT /api/services/{id}/variables", s.putVariables)
	authed("POST /api/services/{id}/domains", s.createDomain)
	authed("DELETE /api/domains/{id}", s.deleteDomain)
	authed("POST /api/services/{id}/volumes", s.createVolume)
	authed("DELETE /api/volumes/{id}", s.deleteVolume)

	authed("GET /api/services/{id}/deployments", s.listDeployments)
	authed("POST /api/services/{id}/deployments", s.createDeployment)
	authed("GET /api/deployments/{id}", s.getDeployment)
	authed("POST /api/deployments/{id}/redeploy", s.redeploy)
	authed("POST /api/deployments/{id}/cancel", s.cancelDeployment)
	authed("GET /api/deployments/{id}/logs", s.deploymentLogs)
	authed("GET /api/services/{id}/logs", s.serviceLogs)
	authed("GET /api/services/{id}/metrics", s.serviceMetrics)
	authed("GET /api/host/metrics", s.hostMetrics)
	authed("GET /api/logs", s.shedLogs)

	authed("GET /api/services/{id}/backups", s.serviceBackups)
	authed("PUT /api/services/{id}/backups/policy", s.putServiceBackupPolicy)
	authed("POST /api/services/{id}/backups", s.runServiceBackup)
	authed("GET /api/backups/system", s.systemBackups)
	authed("PUT /api/backups/system/policy", s.putSystemBackupPolicy)
	authed("POST /api/backups/system", s.runSystemBackup)
	authed("GET /api/backups/settings", s.getBackupSettings)
	authed("PUT /api/backups/settings", s.putBackupSettings)
	authed("POST /api/backups/settings/test", s.testBackupSettings)
	authed("GET /api/backups/settings/key", s.getBackupKey)
	authed("GET /api/backups/{id}/download", s.downloadBackup)
	authed("POST /api/backups/{id}/restore", s.restoreBackup)
	authed("DELETE /api/backups/{id}", s.deleteBackup)

	authed("GET /api/github/repos", s.repos)
	authed("GET /api/github/repos/{owner}/{repo}/branches", s.branches)

	mux.Handle("/api/", s.handle(func(http.ResponseWriter, *http.Request) error {
		return errorf(http.StatusNotFound, "not found")
	}))
	mux.Handle("/", spa(s.web))

	return s.logRequests(securityHeaders(mux))
}

// logRequests logs every request at debug level.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Debug("http request", "method", r.Method, "path", r.URL.Path,
			"status", rec.status, "duration", time.Since(start))
	})
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	u, _ := auth.UserFrom(r.Context())
	return writeJSON(w, http.StatusOK, userJSON{Login: u.Login, Name: u.Name, AvatarURL: u.AvatarURL})
}
