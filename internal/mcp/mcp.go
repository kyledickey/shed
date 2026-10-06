// Package mcp serves shed's Model Context Protocol server, which lets coding
// agents inspect projects, services, deployments, logs, metrics, variable
// names, and backups. Every tool is read-only.
//
// The server speaks the Streamable HTTP transport, statelessly. It does not
// authenticate requests: the caller mounts [Server.Handler] behind bearer
// token and Origin checks.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/update"
)

// Backend is what the tools read. *control.Plane implements it. Its log
// methods mask variable values, and it never returns them otherwise.
type Backend interface {
	ListProjects(ctx context.Context) ([]control.ProjectSummary, error)
	Project(ctx context.Context, id string) (control.ProjectView, error)
	Service(ctx context.Context, id string) (control.ServiceView, error)
	Deployments(ctx context.Context, serviceID string, limit int) ([]control.DeploymentView, error)
	Deployment(ctx context.Context, id string) (control.DeploymentView, error)
	BuildLog(ctx context.Context, deploymentID string, n int) ([]string, error)
	RuntimeLogs(ctx context.Context, serviceID string, n int) ([]string, error)
	ServiceMetrics(ctx context.Context, serviceID, rng string) (metrics.Series, error)
	HostMetrics(ctx context.Context, rng string) (metrics.HostSeries, error)
	VariableNames(ctx context.Context, serviceID string) (control.VariableNames, error)
	ServiceBackups(ctx context.Context, serviceID string) (control.BackupList, error)
	ShedLog(ctx context.Context, n int) ([]string, error)
	UpdateStatus(ctx context.Context) (update.Status, error)
	Repos(ctx context.Context) ([]github.Repo, error)
	Branches(ctx context.Context, owner, repo string) ([]string, error)
}

var _ Backend = (*control.Plane)(nil)

// Config configures a Server.
type Config struct {
	Backend Backend
	// Version is the running version of shed, reported to clients.
	Version string
	Log     *slog.Logger
}

// Server is the MCP server.
type Server struct {
	backend Backend
	log     *slog.Logger
	server  *sdk.Server
}

// New returns a Server with every tool registered.
func New(cfg Config) *Server {
	s := &Server{backend: cfg.Backend, log: cfg.Log}
	s.server = sdk.NewServer(&sdk.Implementation{Name: "shed", Title: "shed", Version: cfg.Version}, &sdk.ServerOptions{
		Instructions: instructions,
		Logger:       cfg.Log,
		Capabilities: &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
	})
	s.addTools()
	return s
}

// maxRequestBytes bounds the body of an MCP request.
const maxRequestBytes = 1 << 20

// Handler returns the Streamable HTTP handler. It is stateless: it answers
// POST requests and refuses GET and DELETE with 405, since there are no
// sessions to stream to or end. Responses are plain JSON.
func (s *Server) Handler() http.Handler {
	return sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.server }, &sdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		Logger:       s.log,
		// shed listens on loopback behind its own proxy, which passes the
		// public Host through; the caller checks Origin and requires a
		// bearer token instead.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxRequestBytes,
	})
}

// fail turns err into a tool error with a message fit for the agent. what
// and id name the object the tool looked up, for not-found errors.
// Unexpected errors are logged and reported generically.
func (s *Server) fail(err error, what, id string) error {
	if e, ok := control.Explain(err); ok {
		if errors.Is(e, control.ErrNotFound) && e.Msg == "not found" && what != "" {
			return fmt.Errorf("%s %q not found", what, id)
		}
		return errors.New(e.Msg)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return errors.New("request canceled")
	}
	s.log.Error("mcp tool failed", "err", err)
	return errors.New("internal error; shed's log has the details")
}

// instructions is sent to clients when they connect.
const instructions = `shed is a self-hosted deployment platform running on one server. These tools are read-only: you can inspect everything, but cannot deploy, restart, or change anything. When a fix needs a change, tell the user which dashboard action to take.

Model:
- A project groups services on one private network. There are no environments.
- A service is an app (built from a GitHub repo and branch, with its Dockerfile or with Railpack, or run from a Docker image) or a database (postgres, mysql, mongo, redis).
- A deployment is one attempt to build or pull an image, start a container beside the old one, health check it, and switch traffic. If it fails, the previous container keeps serving.
- Tools take IDs, not names. Start with list_projects, then get_project or get_service.
- Variables: list_variables returns names only. Values are never available; logs mask them as ***.
- Settings and variables changed in the dashboard apply from the next deployment.

To diagnose a failed deploy: get_service (status, latestDeployment), list_deployments, get_deployment (its error), then build_log for that deployment. Lines starting with "==> " mark the stages; the one before "==> Deployment failed" is the stage that failed. If the container started and then failed or crashed, read runtime_logs and service_metrics (memory against its limit). host_metrics shows whether the server is short on CPU, memory, or disk. shed_log covers shed itself: webhooks, routing, certificates, backups.`
