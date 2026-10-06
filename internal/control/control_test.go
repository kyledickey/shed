package control

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/deploy"
	"github.com/kyledickey/shed/internal/github"
	"github.com/kyledickey/shed/internal/metrics"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/update"
)

// fakeDeployer records deployments and service controls without running
// them.
type fakeDeployer struct {
	mu         sync.Mutex
	deploy     []store.Trigger
	commits    []string // SHA of each deployment
	routeFails int      // how many more ApplyRoutes calls fail
	routeCalls int
	deployErr  error           // returned by Deploy
	deleteErr  error           // returned by DeleteService and DeleteProject
	onDelete   func(id string) // called by them with "delete <id>"
	available  map[string]bool // returned by AvailableImages
	imagesErr  error           // returned by AvailableImages
	resolved   map[string]string
	resolveErr error
	runtimeLog string // written by RuntimeLogs
	follow     []bool // the follow argument of each RuntimeLogs call
	logLines   []int  // the n argument of each BuildLog call
}

func (f *fakeDeployer) Deploy(_ context.Context, serviceID string, trigger store.Trigger, c deploy.Commit) (store.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deployErr != nil {
		return store.Deployment{}, f.deployErr
	}
	f.deploy = append(f.deploy, trigger)
	f.commits = append(f.commits, c.SHA)
	return store.Deployment{ServiceID: serviceID, Trigger: trigger, CommitSHA: c.SHA, Status: store.StatusQueued}, nil
}

func (f *fakeDeployer) setDeployErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deployErr = err
}

func (f *fakeDeployer) deployed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.commits)
}

func (f *fakeDeployer) triggers() []store.Trigger {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deploy)
}

func (f *fakeDeployer) Redeploy(context.Context, string) (store.Deployment, error) {
	return store.Deployment{}, deploy.ErrNoImage
}

func (f *fakeDeployer) AvailableImages(context.Context, string, []string) (map[string]bool, error) {
	return f.available, f.imagesErr
}

func (f *fakeDeployer) Cancel(context.Context, string) (store.Deployment, error) {
	return store.Deployment{}, deploy.ErrNotInProgress
}

func (f *fakeDeployer) ServiceStatuses(context.Context, string) (map[string]deploy.ServiceStatus, error) {
	return map[string]deploy.ServiceStatus{}, nil
}

func (f *fakeDeployer) ApplyRoutes(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routeCalls++
	if f.routeFails > 0 {
		f.routeFails--
		return errors.New("proxy: load config: boom")
	}
	return nil
}

func (f *fakeDeployer) routes() (calls, fails int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routeCalls, f.routeFails
}

func (f *fakeDeployer) StopService(context.Context, string) error       { return nil }
func (f *fakeDeployer) StartService(context.Context, string) error      { return nil }
func (f *fakeDeployer) RestartService(context.Context, string) error    { return nil }
func (f *fakeDeployer) ClearRestoreFence(context.Context, string) error { return nil }

func (f *fakeDeployer) delete(id string) error {
	f.mu.Lock()
	onDelete, err := f.onDelete, f.deleteErr
	f.mu.Unlock()
	if onDelete != nil {
		onDelete("delete " + id)
	}
	return err
}

func (f *fakeDeployer) DeleteService(_ context.Context, id string) error { return f.delete(id) }
func (f *fakeDeployer) DeleteProject(_ context.Context, id string) error { return f.delete(id) }
func (f *fakeDeployer) DeleteVolume(context.Context, string) error       { return nil }

func (f *fakeDeployer) FollowLog(context.Context, string, func(string), func(store.DeploymentStatus)) error {
	return nil
}

func (f *fakeDeployer) BuildLog(_ context.Context, _ string, n int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logLines = append(f.logLines, n)
	return []string{}, nil
}

func (f *fakeDeployer) RuntimeLogs(_ context.Context, _ string, _ int, follow bool, w io.Writer) error {
	f.mu.Lock()
	f.follow = append(f.follow, follow)
	out := f.runtimeLog
	f.mu.Unlock()
	if out == "" {
		return deploy.ErrNoContainer
	}
	_, err := io.WriteString(w, out)
	return err
}

func (f *fakeDeployer) ResolveVariables(context.Context, string) (map[string]string, error) {
	return f.resolved, f.resolveErr
}

// fakeBackups records pauses and the deletions they cover.
type fakeBackups struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeBackups) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeBackups) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeBackups) PauseService(_ context.Context, serviceID string) (func(), error) {
	f.record("pause " + serviceID)
	return func() { f.record("resume " + serviceID) }, nil
}

func (f *fakeBackups) ForgetService(_ context.Context, serviceID string) error {
	f.record("forget " + serviceID)
	return nil
}

func (f *fakeBackups) BackUp(context.Context, string) (store.Backup, error) {
	return store.Backup{}, nil
}
func (f *fakeBackups) Backups(context.Context, string, int) ([]store.Backup, error) { return nil, nil }
func (f *fakeBackups) Restore(context.Context, string) (store.Restore, error) {
	return store.Restore{}, nil
}
func (f *fakeBackups) Delete(context.Context, string) error { return nil }
func (f *fakeBackups) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *fakeBackups) Policy(context.Context, string) (backup.Policy, error) {
	return backup.Policy{}, nil
}
func (f *fakeBackups) SetPolicy(_ context.Context, _ string, in backup.PolicyInput) (backup.Policy, error) {
	return backup.Policy{PolicyInput: in}, nil
}
func (f *fakeBackups) Settings(context.Context) (backup.Settings, error) {
	return backup.Settings{}, nil
}
func (f *fakeBackups) SetSettings(context.Context, backup.SettingsInput) (backup.Settings, error) {
	return backup.Settings{}, nil
}
func (f *fakeBackups) TestS3(context.Context, backup.S3Input) error { return nil }
func (f *fakeBackups) Identity(context.Context) (string, error)     { return "", nil }

// fakeLogs holds fixed lines of shed's log.
type fakeLogs []string

func (l fakeLogs) Lines(n int) []string                      { return l[len(l)-min(n, len(l)):] }
func (l fakeLogs) Follow(context.Context, func(line string)) {}

type fakeMetrics struct{}

func (fakeMetrics) Query(_ context.Context, _ string, r metrics.Range) (metrics.Series, error) {
	return metrics.Series{Range: r}, nil
}
func (fakeMetrics) QueryHost(_ context.Context, r metrics.Range) (metrics.HostSeries, error) {
	return metrics.HostSeries{Range: r}, nil
}

type fakeUpdates struct{}

func (fakeUpdates) Status(context.Context) (update.Status, error)   { return update.Status{}, nil }
func (fakeUpdates) Check(context.Context) (update.Status, error)    { return update.Status{}, nil }
func (fakeUpdates) Download(context.Context) (update.Status, error) { return update.Status{}, nil }
func (fakeUpdates) SetAutoDownload(context.Context, bool) (update.Status, error) {
	return update.Status{}, nil
}
func (fakeUpdates) Install(context.Context) (update.Status, error) { return update.Status{}, nil }

// fakeGitHub has one repository, octo/app, whose branches all point at
// commit "head".
type fakeGitHub struct{}

func (fakeGitHub) Commit(_ context.Context, fullName, ref string) (github.Commit, error) {
	if !strings.EqualFold(fullName, "octo/app") {
		return github.Commit{}, errors.New("github: 404 Not Found")
	}
	return github.Commit{SHA: "head", Message: "Head of " + ref, Author: "Mona"}, nil
}

func (fakeGitHub) Repos(context.Context) ([]github.Repo, error) {
	return []github.Repo{{FullName: "octo/app", DefaultBranch: "main"}}, nil
}

func (fakeGitHub) Branches(_ context.Context, fullName string) ([]string, error) {
	if fullName != "octo/app" {
		return nil, errors.New("github: 404 Not Found")
	}
	return []string{"main", "dev"}, nil
}

type fixture struct {
	t        *testing.T
	st       *store.Store
	deployer *fakeDeployer
	backups  *fakeBackups
	logs     fakeLogs // set by tests after the fixture is built
	github   bool     // whether GitHub is configured
	plane    *Plane
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, store.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fixture{t: t, st: st, deployer: &fakeDeployer{}, backups: &fakeBackups{}}
	f.plane = New(Config{
		Store:    st,
		Deployer: f.deployer,
		Backups:  f.backups,
		Metrics:  fakeMetrics{},
		Logs:     &f.logs,
		Updates:  fakeUpdates{},
		GitHub: func() (GitHub, bool) {
			if f.github {
				return fakeGitHub{}, true
			}
			return nil, false
		},
		BaseURL:    "https://shed.example.com",
		BaseDomain: "Apps.Example.com",
		Log:        slog.New(slog.DiscardHandler),
	})
	return f
}

// createApp creates a project with a repo app, web, that deploys octo/app's
// main branch on push.
func createApp(t *testing.T, st *store.Store) store.Service {
	t.Helper()
	ctx := context.Background()
	p, err := st.CreateProject(ctx, "p-"+store.NewID())
	if err != nil {
		t.Fatal(err)
	}
	svc, err := st.CreateService(ctx, store.Service{
		ProjectID: p.ID, Name: "web", Kind: "app", Repo: "Octo/App", Branch: "main", AutoDeploy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// wantKind fails t unless err is an *Error of kind with a message
// containing msg.
func wantKind(t *testing.T, err, kind error, msg string) {
	t.Helper()
	e, ok := Explain(err)
	if !ok || e.Kind != kind || !strings.Contains(e.Msg, msg) {
		t.Errorf("error = %v, want %v containing %q", err, kind, msg)
	}
}
