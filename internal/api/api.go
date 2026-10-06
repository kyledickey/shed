// Package api serves shed's HTTP interface: the JSON API under /api, the
// server-sent event log streams, the GitHub App setup flow and webhook, and
// the dashboard single-page app.
//
// Handlers are thin: they decode the request, call the control plane, map
// its errors to statuses, and encode the result as the JSON types of the
// design document.
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
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/update"
)

// Control carries out the operations behind the API. *control.Plane
// implements it.
type Control interface {
	ListProjects(ctx context.Context) ([]control.ProjectSummary, error)
	Project(ctx context.Context, id string) (control.ProjectView, error)
	CreateProject(ctx context.Context, name string) (control.ProjectView, error)
	RenameProject(ctx context.Context, id, name string) (control.ProjectView, error)
	DeleteProject(ctx context.Context, id string) error

	CreateService(ctx context.Context, projectID string, req control.NewService) (control.ServiceView, error)
	Service(ctx context.Context, id string) (control.ServiceView, error)
	ServiceRecord(ctx context.Context, id string) (store.Service, error)
	UpdateService(ctx context.Context, id string, patch control.ServicePatch) (control.ServiceView, error)
	DeleteService(ctx context.Context, id string) error
	StopService(ctx context.Context, id string) error
	StartService(ctx context.Context, id string) error
	RestartService(ctx context.Context, id string) error
	ClearRestoreFence(ctx context.Context, id string) error

	Variables(ctx context.Context, serviceID string) (map[string]string, error)
	SetVariables(ctx context.Context, serviceID string, vars map[string]string) (map[string]string, error)
	ResolvedVariables(ctx context.Context, serviceID string) (map[string]string, error)
	CreateDomain(ctx context.Context, serviceID, host string) (store.Domain, bool, error)
	DeleteDomain(ctx context.Context, id string) (bool, error)
	CreateVolume(ctx context.Context, serviceID, mountPath string) (store.Volume, error)
	DeleteVolume(ctx context.Context, id string) error

	Deployments(ctx context.Context, serviceID string, limit int) ([]control.DeploymentView, error)
	Deployment(ctx context.Context, id string) (control.DeploymentView, error)
	Deploy(ctx context.Context, serviceID string) (control.DeploymentView, error)
	Redeploy(ctx context.Context, deploymentID string) (control.DeploymentView, error)
	CancelDeployment(ctx context.Context, deploymentID string) (control.DeploymentView, error)
	FollowBuildLog(ctx context.Context, deploymentID string, line func(string), status func(store.DeploymentStatus)) error
	FollowRuntimeLogs(ctx context.Context, serviceID string, tail int, w io.Writer) error
	FollowShedLog(ctx context.Context, emit func(line string))

	ServicesForPush(ctx context.Context, repo, branch string) ([]store.Service, error)
	ReceivePush(ctx context.Context, push store.PendingPush) error
	DeployPush(ctx context.Context, serviceID string)

	ServiceMetrics(ctx context.Context, serviceID, rng string) (metrics.Series, error)
	HostMetrics(ctx context.Context, rng string) (metrics.HostSeries, error)

	ServiceBackups(ctx context.Context, serviceID string) (control.BackupList, error)
	SystemBackups(ctx context.Context) (control.BackupList, error)
	SetServiceBackupPolicy(ctx context.Context, serviceID string, in backup.PolicyInput) (backup.Policy, error)
	SetSystemBackupPolicy(ctx context.Context, in backup.PolicyInput) (backup.Policy, error)
	BackUpService(ctx context.Context, serviceID string) (control.BackupView, error)
	BackUpSystem(ctx context.Context) (control.BackupView, error)
	OpenBackup(ctx context.Context, id string) (io.ReadCloser, string, error)
	RestoreBackup(ctx context.Context, id string) (store.Restore, error)
	DeleteBackup(ctx context.Context, id string) error
	BackupSettings(ctx context.Context) (backup.Settings, error)
	SetBackupSettings(ctx context.Context, in backup.SettingsInput) (backup.Settings, error)
	TestBackupS3(ctx context.Context, in backup.S3Input) error
	BackupIdentity(ctx context.Context) (string, error)

	UpdateStatus(ctx context.Context) (update.Status, error)
	CheckUpdate(ctx context.Context) (update.Status, error)
	DownloadUpdate(ctx context.Context) (update.Status, error)
	SetAutoDownload(ctx context.Context, on bool) (update.Status, error)
	InstallUpdate(ctx context.Context) (update.Status, error)

	Repos(ctx context.Context) ([]github.Repo, error)
	Branches(ctx context.Context, owner, repo string) ([]string, error)
}

var _ Control = (*control.Plane)(nil)

// Settings stores the GitHub App credentials. *store.Store implements it.
type Settings interface {
	Setting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

var _ Settings = (*store.Store)(nil)

// Config configures a Server.
type Config struct {
	Control  Control
	Settings Settings
	// Restart shuts shed down gracefully and starts it again. The API calls
	// it after installing an update.
	Restart func()
	Auth    *auth.Auth
	// GitHub holds the GitHub client. New fills it from the stored App
	// credentials, and the setup flow fills it once the App is created.
	GitHub *GitHubHolder
	// BaseURL is the public dashboard URL.
	BaseURL string
	// Web holds the built dashboard, with index.html at its root.
	Web fs.FS
	// HTTPClient is used for GitHub API calls. Nil means a default client.
	HTTPClient *http.Client
	Log        *slog.Logger
}

// Server is the HTTP interface.
type Server struct {
	control    Control
	settings   Settings
	restart    func()
	auth       *auth.Auth
	github     *GitHubHolder
	baseURL    string
	web        fs.FS
	httpClient *http.Client
	log        *slog.Logger

	webhooks   webhookGuard
	deliveries deliveryCache
	setupMu    sync.Mutex // serializes completing the GitHub setup
	tokenMu    sync.Mutex
	setupToken string // guarded by tokenMu; empty once GitHub is configured
}

// New returns a Server. It loads the GitHub App from the store, or, if none is
// configured, generates the one-time setup token and logs it.
func New(ctx context.Context, cfg Config) (*Server, error) {
	s := &Server{
		control:    cfg.Control,
		settings:   cfg.Settings,
		restart:    cfg.Restart,
		auth:       cfg.Auth,
		github:     cfg.GitHub,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		web:        cfg.Web,
		httpClient: cfg.HTTPClient,
		log:        cfg.Log,
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
	authed("POST /api/services/{id}/stop", s.controlService(Control.StopService))
	authed("POST /api/services/{id}/start", s.controlService(Control.StartService))
	authed("POST /api/services/{id}/restart", s.controlService(Control.RestartService))
	authed("POST /api/services/{id}/restore-fence/clear", s.controlService(Control.ClearRestoreFence))

	authed("GET /api/services/{id}/variables", s.getVariables)
	authed("PUT /api/services/{id}/variables", s.putVariables)
	authed("GET /api/services/{id}/variables/resolved", s.getResolvedVariables)
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

	authed("GET /api/update", s.getUpdate)
	authed("POST /api/update/check", s.checkUpdate)
	authed("POST /api/update/download", s.downloadUpdate)
	authed("PUT /api/update/settings", s.putUpdateSettings)
	authed("POST /api/update/install", s.installUpdate)

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
