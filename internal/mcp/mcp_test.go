package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/control"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/update"
)

// secret is a variable value that must never reach a client.
const secret = "s3cr3t-value-123"

var created = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// fakeBackend serves one project with one service, svc1, and its
// deployment dep1. Other IDs are not found.
type fakeBackend struct {
	lines   []int // line counts the log methods were asked for
	logs    []string
	fail    error // returned by every method when set
	repos   int
	backups int
}

func (f *fakeBackend) check(id, want string) error {
	if f.fail != nil {
		return f.fail
	}
	if id != want {
		return store.ErrNotFound
	}
	return nil
}

func (f *fakeBackend) deployment() control.DeploymentView {
	return control.DeploymentView{
		ID: "dep1", ServiceID: "svc1", Status: store.StatusFailed, Trigger: store.TriggerPush,
		CommitSHA: "abc123", Error: "health check failed", CreatedAt: created,
	}
}

func (f *fakeBackend) service() control.ServiceView {
	d := f.deployment()
	return control.ServiceView{
		Service: store.Service{
			ID: "svc1", ProjectID: "prj1", Name: "web", Kind: "app", Repo: "acme/web", Branch: "main",
			Port: 8080, MemoryLimit: 512 << 20, AutoDeploy: true, CreatedAt: created,
		},
		Status:           deploy.StatusFailed,
		Domains:          []store.Domain{{ID: "dom1", Host: "web.example.com"}},
		LatestDeployment: &d,
	}
}

func (f *fakeBackend) ListProjects(context.Context) ([]control.ProjectSummary, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return []control.ProjectSummary{{
		Project:  store.Project{ID: "prj1", Name: "acme", CreatedAt: created},
		Services: []control.ServiceSummary{{ID: "svc1", Name: "web", Kind: "app", Status: deploy.StatusActive}},
	}}, nil
}

func (f *fakeBackend) Project(_ context.Context, id string) (control.ProjectView, error) {
	if err := f.check(id, "prj1"); err != nil {
		return control.ProjectView{}, err
	}
	return control.ProjectView{
		Project:  store.Project{ID: "prj1", Name: "acme", CreatedAt: created},
		Services: []control.ServiceView{f.service()},
	}, nil
}

func (f *fakeBackend) Service(_ context.Context, id string) (control.ServiceView, error) {
	if err := f.check(id, "svc1"); err != nil {
		return control.ServiceView{}, err
	}
	return f.service(), nil
}

func (f *fakeBackend) Deployments(_ context.Context, serviceID string, limit int) ([]control.DeploymentView, error) {
	if err := f.check(serviceID, "svc1"); err != nil {
		return nil, err
	}
	f.lines = append(f.lines, limit)
	return []control.DeploymentView{f.deployment()}, nil
}

func (f *fakeBackend) Deployment(_ context.Context, id string) (control.DeploymentView, error) {
	if err := f.check(id, "dep1"); err != nil {
		return control.DeploymentView{}, err
	}
	return f.deployment(), nil
}

func (f *fakeBackend) logLines(n int) []string {
	f.lines = append(f.lines, n)
	if f.logs != nil {
		return f.logs
	}
	return []string{"==> Building", "token=***", "==> Deployment failed: health check failed"}
}

func (f *fakeBackend) BuildLog(_ context.Context, id string, n int) ([]string, error) {
	if err := f.check(id, "dep1"); err != nil {
		return nil, err
	}
	return f.logLines(n), nil
}

func (f *fakeBackend) RuntimeLogs(_ context.Context, id string, n int) ([]string, error) {
	if err := f.check(id, "svc1"); err != nil {
		return nil, err
	}
	return f.logLines(n), nil
}

func (f *fakeBackend) ShedLog(_ context.Context, n int) ([]string, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return f.logLines(n), nil
}

func ptr(v float64) *float64 { return &v }

func (f *fakeBackend) ServiceMetrics(_ context.Context, id, rng string) (metrics.Series, error) {
	if err := f.check(id, "svc1"); err != nil {
		return metrics.Series{}, err
	}
	return metrics.Series{
		Range: metrics.Range(rng), Start: created, Step: time.Minute, MemoryLimit: 512 << 20,
		CPU:    []*float64{ptr(10), nil, ptr(30), ptr(20), nil},
		Memory: []*float64{ptr(100), ptr(300)},
	}, nil
}

func (f *fakeBackend) HostMetrics(_ context.Context, rng string) (metrics.HostSeries, error) {
	if f.fail != nil {
		return metrics.HostSeries{}, f.fail
	}
	return metrics.HostSeries{Range: metrics.Range(rng), Start: created, Step: time.Minute, CPUs: 4, DiskUsed: []*float64{ptr(5)}}, nil
}

// VariableNames derives the names from variables that have values, as the
// control plane does.
func (f *fakeBackend) VariableNames(_ context.Context, id string) (control.VariableNames, error) {
	if err := f.check(id, "svc1"); err != nil {
		return control.VariableNames{}, err
	}
	own := map[string]string{"API_TOKEN": secret, "DATABASE_URL": "${{ db.DATABASE_URL }}"}
	var out control.VariableNames
	for name, value := range own {
		out.Variables = append(out.Variables, control.VariableName{Name: name, Reference: strings.Contains(value, "${{")})
	}
	out.Variables = append(out.Variables, control.VariableName{Name: "PORT", Injected: true})
	return out, nil
}

func (f *fakeBackend) ServiceBackups(_ context.Context, id string) (control.BackupList, error) {
	if err := f.check(id, "svc1"); err != nil {
		return control.BackupList{}, err
	}
	out := control.BackupList{Policy: backup.Policy{PolicyInput: backup.PolicyInput{Enabled: true, Schedule: "0 3 * * *", KeepLocal: 7}}}
	for i := range f.backups {
		out.Backups = append(out.Backups, control.BackupView{Backup: store.Backup{
			ID: fmt.Sprintf("bk%d", i), ServiceID: "svc1", Status: store.BackupSucceeded, RemoteKey: "key", CreatedAt: created,
		}})
	}
	return out, nil
}

func (f *fakeBackend) UpdateStatus(context.Context) (update.Status, error) {
	if f.fail != nil {
		return update.Status{}, f.fail
	}
	return update.Status{Current: "v1.0.0", Latest: &update.Release{Version: "v1.1.0"}, Available: true, State: update.Idle}, nil
}

func (f *fakeBackend) Repos(context.Context) ([]github.Repo, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	repos := make([]github.Repo, f.repos)
	for i := range repos {
		repos[i] = github.Repo{FullName: fmt.Sprintf("acme/r%d", i), DefaultBranch: "main"}
	}
	return repos, nil
}

func (f *fakeBackend) Branches(_ context.Context, owner, repo string) ([]string, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	if owner+"/"+repo != "acme/web" {
		return nil, &control.Error{Kind: control.ErrInvalid, Msg: "repository must be owner/name"}
	}
	return []string{"main", "dev"}, nil
}

// connect serves backend over HTTP and returns a connected client session.
func connect(t *testing.T, backend Backend) *sdk.ClientSession {
	t.Helper()
	srv := httptest.NewServer(New(Config{Backend: backend, Version: "test", Log: slog.New(slog.DiscardHandler)}).Handler())
	t.Cleanup(srv.Close)
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{
		Endpoint: srv.URL, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool and returns its text and whether it is an error.
func call(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String(), res.IsError
}

func TestToolList(t *testing.T) {
	cs := connect(t, &fakeBackend{})
	if !strings.Contains(cs.InitializeResult().Instructions, "list_projects") {
		t.Errorf("instructions = %q", cs.InitializeResult().Instructions)
	}
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	slices.Sort(names)
	want := []string{
		"build_log", "get_deployment", "get_project", "get_service", "host_metrics", "list_backups",
		"list_branches", "list_deployments", "list_projects", "list_repos", "list_variables",
		"runtime_logs", "service_metrics", "shed_log", "update_status",
	}
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v", names, want)
	}
}

func TestTools(t *testing.T) {
	tests := []struct {
		tool string
		args map[string]any
		want []string // substrings of the result text
	}{
		{"list_projects", nil, []string{`"id":"prj1"`, `"status":"active"`}},
		{"get_project", map[string]any{"project_id": "prj1"}, []string{`"name":"acme"`, `"repo":"acme/web"`}},
		{"get_service", map[string]any{"service_id": "svc1"}, []string{`"port":8080`, `"host":"web.example.com"`, `"latestDeployment":{"commitSha":"abc123"`}},
		{"list_deployments", map[string]any{"service_id": "svc1"}, []string{`"id":"dep1"`, `"error":"health check failed"`}},
		{"get_deployment", map[string]any{"deployment_id": "dep1"}, []string{`"status":"failed"`, `"commitSha":"abc123"`}},
		{"build_log", map[string]any{"deployment_id": "dep1"}, []string{"==> Building\ntoken=***\n==> Deployment failed"}},
		{"runtime_logs", map[string]any{"service_id": "svc1", "lines": 10}, []string{"token=***"}},
		{"shed_log", nil, []string{"==> Building"}},
		{"service_metrics", map[string]any{"service_id": "svc1", "range": "6h"}, []string{
			`"range":"6h"`, `"cpuPercent":{"avg":20,"latest":20,"max":30}`, `"memoryBytes":{"avg":200,"latest":300,"max":300}`,
			`"netRxBytesPerSecond":{"avg":null,"latest":null,"max":null}`, `"stepSeconds":60`,
		}},
		{"host_metrics", nil, []string{`"cpus":4`, `"diskUsedBytes":{"avg":5,"latest":5,"max":5}`}},
		{"list_variables", map[string]any{"service_id": "svc1"}, []string{`"name":"API_TOKEN"`, `{"injected":true,"name":"PORT"}`, `"name":"DATABASE_URL","reference":true`}},
		{"list_backups", map[string]any{"service_id": "svc1"}, []string{`"schedule":"0 3 * * *"`, `"id":"bk0"`, `"remote":true`}},
		{"update_status", nil, []string{`"current":"v1.0.0"`, `"version":"v1.1.0"`, `"available":true`}},
		{"list_repos", nil, []string{`"fullName":"acme/r0"`}},
		{"list_branches", map[string]any{"owner": "acme", "repo": "web"}, []string{`"branches":["main","dev"]`}},
	}
	cs := connect(t, &fakeBackend{repos: 1, backups: 1})
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			text, isErr := call(t, cs, tt.tool, tt.args)
			if isErr {
				t.Fatalf("error: %s", text)
			}
			for _, w := range tt.want {
				if !strings.Contains(text, w) {
					t.Errorf("result lacks %s:\n%s", w, text)
				}
			}
		})
	}
}

func TestToolErrors(t *testing.T) {
	tests := []struct {
		name string
		fail error
		tool string
		args map[string]any
		want string
	}{
		{"missing project", nil, "get_project", map[string]any{"project_id": "nope"}, `project "nope" not found`},
		{"missing service", nil, "get_service", map[string]any{"service_id": "nope"}, `service "nope" not found`},
		{"missing deployment log", nil, "build_log", map[string]any{"deployment_id": "nope"}, `deployment "nope" not found`},
		{"missing service logs", nil, "runtime_logs", map[string]any{"service_id": "nope"}, `service "nope" not found`},
		{"missing variables", nil, "list_variables", map[string]any{"service_id": "nope"}, `service "nope" not found`},
		{"invalid repo", nil, "list_branches", map[string]any{"owner": "a b", "repo": "c"}, "repository must be owner/name"},
		{"bad range", nil, "service_metrics", map[string]any{"service_id": "svc1", "range": "2h"}, "range"},
		{"missing argument", nil, "get_service", nil, "service_id"},
		{"no container", deploy.ErrNoContainer, "runtime_logs", map[string]any{"service_id": "svc1"}, "nothing to run; deploy the service first"},
		{"conflict", &control.Error{Kind: control.ErrConflict, Msg: "GitHub App is not configured"}, "list_repos", nil, "GitHub App is not configured"},
		{"internal", errors.New("sqlite: disk I/O error at /var/lib/shed/shed.db"), "list_projects", nil, "internal error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := connect(t, &fakeBackend{fail: tt.fail})
			text, isErr := call(t, cs, tt.tool, tt.args)
			if !isErr {
				t.Fatalf("not an error: %s", text)
			}
			if !strings.Contains(text, tt.want) {
				t.Errorf("error = %q, want it to contain %q", text, tt.want)
			}
			if strings.Contains(text, "sqlite") || strings.Contains(text, "/var/lib") {
				t.Errorf("error leaks internals: %q", text)
			}
		})
	}
}

func TestListVariablesReturnsNoValues(t *testing.T) {
	cs := connect(t, &fakeBackend{})
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "list_variables", Arguments: map[string]any{"service_id": "svc1"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "db.DATABASE_URL") {
		t.Errorf("list_variables returned a value: %s", raw)
	}
	var out variablesOut
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil || len(out.Variables) != 3 {
		t.Errorf("variables = %s (%v)", b, err)
	}
}

func TestLogLineCounts(t *testing.T) {
	tests := []struct {
		lines any
		want  int
	}{
		{nil, defaultLogLines},
		{0, defaultLogLines},
		{-5, defaultLogLines},
		{50, 50},
		{5000, maxLogLines},
	}
	for _, tt := range tests {
		f := &fakeBackend{}
		cs := connect(t, f)
		args := map[string]any{"deployment_id": "dep1"}
		if tt.lines != nil {
			args["lines"] = tt.lines
		}
		if text, isErr := call(t, cs, "build_log", args); isErr {
			t.Fatalf("lines %v: %s", tt.lines, text)
		}
		if !slices.Equal(f.lines, []int{tt.want}) {
			t.Errorf("lines %v: asked for %v, want %d", tt.lines, f.lines, tt.want)
		}
	}
}

func TestListCaps(t *testing.T) {
	cs := connect(t, &fakeBackend{repos: maxListItems + 3, backups: maxBackups + 2})
	text, _ := call(t, cs, "list_repos", nil)
	var repos reposOut
	if err := json.Unmarshal([]byte(text), &repos); err != nil || len(repos.Repos) != maxListItems || repos.Omitted != 3 {
		t.Errorf("repos: %d listed, %d omitted (%v)", len(repos.Repos), repos.Omitted, err)
	}
	text, _ = call(t, cs, "list_backups", map[string]any{"service_id": "svc1"})
	var backups backupsOut
	if err := json.Unmarshal([]byte(text), &backups); err != nil || len(backups.Backups) != maxBackups || backups.Omitted != 2 {
		t.Errorf("backups: %d listed, %d omitted (%v)", len(backups.Backups), backups.Omitted, err)
	}
	f := &fakeBackend{}
	cs = connect(t, f)
	call(t, cs, "list_deployments", map[string]any{"service_id": "svc1", "limit": 500})
	call(t, cs, "list_deployments", map[string]any{"service_id": "svc1"})
	if !slices.Equal(f.lines, []int{maxDeployments, defaultDeployments}) {
		t.Errorf("deployment limits = %v", f.lines)
	}
}

func TestLogResultBounds(t *testing.T) {
	text := func(lines []string) string {
		return logResult(lines).Content[0].(*sdk.TextContent).Text
	}
	if got := text(nil); got != "(no log lines)" {
		t.Errorf("empty = %q", got)
	}
	long := strings.Repeat("é", maxLogLineRunes+10)
	if got := text([]string{long}); !strings.HasSuffix(got, " [line cut]") || len([]rune(got)) != maxLogLineRunes+len(" [line cut]") {
		t.Errorf("long line kept %d runes", len([]rune(got)))
	}
	lines := make([]string, maxLogLines)
	for i := range lines {
		lines[i] = fmt.Sprintf("%04d %s", i, strings.Repeat("x", 1000))
	}
	got := text(lines)
	if len(got) > maxLogBytes+100 {
		t.Errorf("output is %d bytes", len(got))
	}
	if !strings.HasPrefix(got, "[") || !strings.Contains(got, "older lines omitted") || !strings.HasSuffix(got, lines[len(lines)-1]) {
		t.Errorf("output does not keep the newest lines: %q...", got[:80])
	}
}

func TestHandlerMethods(t *testing.T) {
	h := New(Config{Backend: &fakeBackend{}, Log: slog.New(slog.DiscardHandler)}).Handler()
	for _, method := range []string{"GET", "DELETE"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/mcp", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
	}
}
