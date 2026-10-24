package mcp

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Bounds on what the tools return.
const (
	defaultLogLines    = 200
	maxLogLines        = 2000
	maxLogLineRunes    = 2000      // longer lines are cut
	maxLogBytes        = 256 << 10 // older lines are dropped beyond this
	defaultDeployments = 20
	maxDeployments     = 50
	maxBackups         = 20
	maxListItems       = 500 // repositories and branches
)

type none struct{}

type projectArgs struct {
	ProjectID string `json:"project_id" jsonschema:"ID of the project, from list_projects"`
}

type serviceArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of the service, from list_projects or get_project"`
}

type deploymentsArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of the service, from list_projects or get_project"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many deployments to return, newest first; default 20, at most 50"`
}

type deploymentArgs struct {
	DeploymentID string `json:"deployment_id" jsonschema:"ID of the deployment, from list_deployments or a service's latestDeployment"`
}

type buildLogArgs struct {
	DeploymentID string `json:"deployment_id" jsonschema:"ID of the deployment, from list_deployments or a service's latestDeployment"`
	Lines        int    `json:"lines,omitempty" jsonschema:"how many of the last lines to return; default 200, at most 2000"`
}

type runtimeLogsArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of the service, from list_projects or get_project"`
	Lines     int    `json:"lines,omitempty" jsonschema:"how many of the last lines to return; default 200, at most 2000"`
}

type serviceMetricsArgs struct {
	ServiceID string `json:"service_id" jsonschema:"ID of the service, from list_projects or get_project"`
	Range     string `json:"range,omitempty" jsonschema:"time range to summarize: 1h, 6h, 24h, or 7d; default 1h"`
}

type hostMetricsArgs struct {
	Range string `json:"range,omitempty" jsonschema:"time range to summarize: 1h, 6h, 24h, or 7d; default 1h"`
}

type shedLogArgs struct {
	Lines int `json:"lines,omitempty" jsonschema:"how many of the last lines to return; default 200, at most 2000"`
}

type branchesArgs struct {
	Owner string `json:"owner" jsonschema:"owner of the repository, the part before the slash in its full name"`
	Repo  string `json:"repo" jsonschema:"name of the repository, the part after the slash in its full name"`
}

// addTools registers every tool.
func (s *Server) addTools() {
	add(s, "list_projects", "List projects",
		"Lists every project with its services: each service's ID, name, kind (app, postgres, mysql, mongo, redis), and status. Start here to find IDs.",
		s.listProjects)
	add(s, "get_project", "Get project",
		"Returns one project with its services in full: settings, status, domains, volumes, and latest deployment.",
		s.getProject)
	add(s, "get_service", "Get service",
		"Returns one service in full: kind, repo, branch, image, build and runtime settings, resource limits, status, domains, volumes, latest deployment, and any restore fence. Status is active, deploying, crashed, failed, stopped, or offline.",
		s.getService)
	add(s, "list_deployments", "List deployments",
		"Lists a service's deployments, newest first, with status, trigger, commit, image, and error. shed keeps the newest 50.",
		s.listDeployments)
	add(s, "get_deployment", "Get deployment",
		"Returns one deployment: status, trigger, commit, image, timing, and the error it failed with, if any.",
		s.getDeployment)
	add(s, "build_log", "Build log",
		"Returns the last lines of a deployment's build log as text, with variable values masked as ***. Lines starting with \"==> \" mark the stages of the deployment.",
		s.buildLog)
	add(s, "runtime_logs", "Runtime logs",
		"Returns a snapshot of the last lines of the output of a service's running container, each starting with its timestamp, with variable values masked as ***. It does not follow the log.",
		s.runtimeLogs)
	addWithSchema(s, "service_metrics", "Service metrics",
		"Summarizes a service's CPU, memory, network, and disk I/O over a time range: the latest, average, and maximum of each, and its limits. CPU is in percent of one core, memory in bytes, and the rest in bytes per second. Values are null where nothing was sampled.",
		s.serviceMetrics, rangeEnum)
	addWithSchema(s, "host_metrics", "Host metrics",
		"Summarizes the server's CPU, memory, disk use, network, and disk I/O over a time range: the latest, average, and maximum of each, and its capacity. CPU is in percent of one core, memory and disk use in bytes, and the rest in bytes per second.",
		s.hostMetrics, rangeEnum)
	add(s, "list_variables", "List variable names",
		"Lists the names of the variables a service's next deployment would get: its own, marked reference when they use ${{ }} references, and the ones shed injects, marked injected. Values are never returned.",
		s.listVariables)
	add(s, "list_backups", "List backups",
		"Returns a service's backup policy (schedule, retention, next run), its newest backups with status and errors, and its latest restore.",
		s.listBackups)
	add(s, "shed_log", "shed log",
		"Returns the last lines of shed's own application log as text, with the values of every service's saved variables masked as ***. Use it for problems in shed itself: webhooks, routing, certificates, backups, updates.",
		s.shedLog)
	add(s, "update_status", "Update status",
		"Returns the running version of shed, the latest release, and whether an update is available or downloaded.",
		s.updateStatus)
	add(s, "list_repos", "List repositories",
		"Lists the GitHub repositories the shed GitHub App is installed on, with their default branches.",
		s.listRepos)
	add(s, "list_branches", "List branches",
		"Lists the branches of a GitHub repository the shed GitHub App is installed on.",
		s.listBranches)
}

// add registers a read-only tool.
func add[In, Out any](s *Server, name, title, description string, h sdk.ToolHandlerFor[In, Out]) {
	sdk.AddTool(s.server, readOnly(name, title, description), h)
}

// addWithSchema registers a read-only tool after edit adjusts the schema
// inferred from In.
func addWithSchema[In, Out any](s *Server, name, title, description string, h sdk.ToolHandlerFor[In, Out], edit func(*jsonschema.Schema)) {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("mcp: schema of %s: %v", name, err))
	}
	edit(schema)
	t := readOnly(name, title, description)
	t.InputSchema = schema
	sdk.AddTool(s.server, t, h)
}

func readOnly(name, title, description string) *sdk.Tool {
	return &sdk.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}
}

// rangeEnum limits the range argument to the supported ranges.
func rangeEnum(s *jsonschema.Schema) {
	s.Properties["range"].Enum = []any{"1h", "6h", "24h", "7d"}
}

func (s *Server) listProjects(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, projectsOut, error) {
	projects, err := s.backend.ListProjects(ctx)
	if err != nil {
		return nil, projectsOut{}, s.fail(err, "", "")
	}
	return nil, toProjects(projects), nil
}

func (s *Server) getProject(ctx context.Context, _ *sdk.CallToolRequest, in projectArgs) (*sdk.CallToolResult, projectOut, error) {
	p, err := s.backend.Project(ctx, in.ProjectID)
	if err != nil {
		return nil, projectOut{}, s.fail(err, "project", in.ProjectID)
	}
	return nil, toProject(p), nil
}

func (s *Server) getService(ctx context.Context, _ *sdk.CallToolRequest, in serviceArgs) (*sdk.CallToolResult, serviceOut, error) {
	v, err := s.backend.Service(ctx, in.ServiceID)
	if err != nil {
		return nil, serviceOut{}, s.fail(err, "service", in.ServiceID)
	}
	return nil, toService(v), nil
}

func (s *Server) listDeployments(ctx context.Context, _ *sdk.CallToolRequest, in deploymentsArgs) (*sdk.CallToolResult, deploymentsOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultDeployments
	}
	deps, err := s.backend.Deployments(ctx, in.ServiceID, min(limit, maxDeployments))
	if err != nil {
		return nil, deploymentsOut{}, s.fail(err, "service", in.ServiceID)
	}
	out := deploymentsOut{Deployments: make([]deploymentOut, 0, len(deps))}
	for _, d := range deps {
		out.Deployments = append(out.Deployments, toDeployment(d))
	}
	return nil, out, nil
}

func (s *Server) getDeployment(ctx context.Context, _ *sdk.CallToolRequest, in deploymentArgs) (*sdk.CallToolResult, deploymentOut, error) {
	d, err := s.backend.Deployment(ctx, in.DeploymentID)
	if err != nil {
		return nil, deploymentOut{}, s.fail(err, "deployment", in.DeploymentID)
	}
	return nil, toDeployment(d), nil
}

func (s *Server) buildLog(ctx context.Context, _ *sdk.CallToolRequest, in buildLogArgs) (*sdk.CallToolResult, any, error) {
	lines, err := s.backend.BuildLog(ctx, in.DeploymentID, logLines(in.Lines))
	if err != nil {
		return nil, nil, s.fail(err, "deployment", in.DeploymentID)
	}
	return logResult(lines), nil, nil
}

func (s *Server) runtimeLogs(ctx context.Context, _ *sdk.CallToolRequest, in runtimeLogsArgs) (*sdk.CallToolResult, any, error) {
	lines, err := s.backend.RuntimeLogs(ctx, in.ServiceID, logLines(in.Lines))
	if err != nil {
		return nil, nil, s.fail(err, "service", in.ServiceID)
	}
	return logResult(lines), nil, nil
}

func (s *Server) shedLog(ctx context.Context, _ *sdk.CallToolRequest, in shedLogArgs) (*sdk.CallToolResult, any, error) {
	lines, err := s.backend.ShedLog(ctx, logLines(in.Lines))
	if err != nil {
		return nil, nil, s.fail(err, "", "")
	}
	return logResult(lines), nil, nil
}

func (s *Server) serviceMetrics(ctx context.Context, _ *sdk.CallToolRequest, in serviceMetricsArgs) (*sdk.CallToolResult, serviceMetricsOut, error) {
	series, err := s.backend.ServiceMetrics(ctx, in.ServiceID, in.Range)
	if err != nil {
		return nil, serviceMetricsOut{}, s.fail(err, "service", in.ServiceID)
	}
	return nil, toServiceMetrics(series), nil
}

func (s *Server) hostMetrics(ctx context.Context, _ *sdk.CallToolRequest, in hostMetricsArgs) (*sdk.CallToolResult, hostMetricsOut, error) {
	series, err := s.backend.HostMetrics(ctx, in.Range)
	if err != nil {
		return nil, hostMetricsOut{}, s.fail(err, "", "")
	}
	return nil, toHostMetrics(series), nil
}

func (s *Server) listVariables(ctx context.Context, _ *sdk.CallToolRequest, in serviceArgs) (*sdk.CallToolResult, variablesOut, error) {
	names, err := s.backend.VariableNames(ctx, in.ServiceID)
	if err != nil {
		return nil, variablesOut{}, s.fail(err, "service", in.ServiceID)
	}
	return nil, toVariables(names), nil
}

func (s *Server) listBackups(ctx context.Context, _ *sdk.CallToolRequest, in serviceArgs) (*sdk.CallToolResult, backupsOut, error) {
	list, err := s.backend.ServiceBackups(ctx, in.ServiceID)
	if err != nil {
		return nil, backupsOut{}, s.fail(err, "service", in.ServiceID)
	}
	return nil, toBackups(list), nil
}

func (s *Server) updateStatus(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, updateOut, error) {
	st, err := s.backend.UpdateStatus(ctx)
	if err != nil {
		return nil, updateOut{}, s.fail(err, "", "")
	}
	return nil, toUpdate(st), nil
}

func (s *Server) listRepos(ctx context.Context, _ *sdk.CallToolRequest, _ none) (*sdk.CallToolResult, reposOut, error) {
	repos, err := s.backend.Repos(ctx)
	if err != nil {
		return nil, reposOut{}, s.fail(err, "", "")
	}
	out := reposOut{Repos: make([]repoOut, 0, min(len(repos), maxListItems))}
	for _, r := range repos[:min(len(repos), maxListItems)] {
		out.Repos = append(out.Repos, repoOut{FullName: r.FullName, DefaultBranch: r.DefaultBranch, Private: r.Private})
	}
	out.Omitted = len(repos) - len(out.Repos)
	return nil, out, nil
}

func (s *Server) listBranches(ctx context.Context, _ *sdk.CallToolRequest, in branchesArgs) (*sdk.CallToolResult, branchesOut, error) {
	branches, err := s.backend.Branches(ctx, in.Owner, in.Repo)
	if err != nil {
		return nil, branchesOut{}, s.fail(err, "repository", in.Owner+"/"+in.Repo)
	}
	n := min(len(branches), maxListItems)
	return nil, branchesOut{Branches: append([]string{}, branches[:n]...), Omitted: len(branches) - n}, nil
}

// logLines returns the number of lines to ask for: lines, or the default if
// it is not positive, at most maxLogLines.
func logLines(lines int) int {
	if lines <= 0 {
		return defaultLogLines
	}
	return min(lines, maxLogLines)
}

// logResult returns lines as one text block. Lines longer than
// maxLogLineRunes are cut, and the oldest lines are dropped to keep the text
// within maxLogBytes.
func logResult(lines []string) *sdk.CallToolResult {
	kept := make([]string, 0, len(lines))
	size := 0
	for i := len(lines) - 1; i >= 0; i-- {
		l := cut(lines[i])
		if size+len(l)+1 > maxLogBytes {
			break
		}
		size += len(l) + 1
		kept = append(kept, l)
	}
	var b strings.Builder
	if dropped := len(lines) - len(kept); dropped > 0 {
		fmt.Fprintf(&b, "[%d older lines omitted to bound the output]\n", dropped)
	}
	for i := len(kept) - 1; i >= 0; i-- {
		b.WriteString(kept[i])
		if i > 0 {
			b.WriteByte('\n')
		}
	}
	if len(lines) == 0 {
		b.WriteString("(no log lines)")
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: b.String()}}}
}

// cut shortens a line to maxLogLineRunes.
func cut(line string) string {
	if utf8.RuneCountInString(line) <= maxLogLineRunes {
		return line
	}
	return string([]rune(line)[:maxLogLineRunes]) + " [line cut]"
}
