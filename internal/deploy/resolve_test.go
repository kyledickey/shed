package deploy

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/store"
)

func TestResolveVariables(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	db, err := f.st.CreateService(ctx, store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, db.ID, map[string]string{"URL": "pg://${{ SHED_PRIVATE_DOMAIN }}"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{
		"GREETING": "hi from ${{ SHED_SERVICE_NAME }}",
		"DB":       "${{ db.URL }}",
		"SHA":      "${{ SHED_GIT_COMMIT_SHA }}",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"GREETING": "hi from web", "DB": "pg://db", "SHA": ""}
	for k, v := range want {
		if got.Env[k] != v {
			t.Errorf("before deploy: %s = %q, want %q", k, got.Env[k], v)
		}
	}

	f.wait(t, f.deploy(t).ID, terminal)
	got, err = f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["SHA"] != "abc1234def" {
		t.Errorf("after deploy: SHA = %q, want the active commit", got.Env["SHA"])
	}
}

// fakeVault keeps the variables of services as 1Password would while it is
// connected, and counts reads.
type fakeVault struct {
	mu    sync.Mutex
	vars  map[string]map[string]string // by service name
	reads map[string]int
	err   error
}

func (f *fakeVault) Get(_ context.Context, svc store.Service) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads[svc.Name]++
	if f.err != nil {
		return nil, f.err
	}
	return maps.Clone(f.vars[svc.Name]), nil
}

func (f *fakeVault) set(name string, vars map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vars[name] = vars
}

func (f *fakeVault) readCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[name]
}

// newVaultFixture keeps the variables of web and a new service db in a
// fakeVault.
func newVaultFixture(t *testing.T) (*fixture, *fakeVault) {
	t.Helper()
	f := newFixture(t)
	vault := &fakeVault{
		vars: map[string]map[string]string{
			"web": {"SECRET": "vault-web-secret", "GREETING": "hi from ${{ SHED_SERVICE_NAME }}"},
			"db":  {"PASSWORD": "vault-db-secret", "URL": "pg://${{ SHED_PRIVATE_DOMAIN }}"},
		},
		reads: make(map[string]int),
	}
	f.d.variables = vault
	if _, err := f.st.CreateService(context.Background(), store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "postgres"}); err != nil {
		t.Fatal(err)
	}
	return f, vault
}

func TestResolveVariablesReadsOtherServicesLazily(t *testing.T) {
	f, vault := newVaultFixture(t)
	ctx := context.Background()

	got, err := f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Env["SECRET"] != "vault-web-secret" || got.Env["GREETING"] != "hi from web" {
		t.Errorf("Env = %v, want web's variables from the vault", got.Env)
	}
	if want := []string{"GREETING", "SECRET"}; !slices.Equal(got.Own, want) {
		t.Errorf("Own = %q, want %q", got.Own, want)
	}
	if got.Saved["GREETING"] != "hi from ${{ SHED_SERVICE_NAME }}" {
		t.Errorf("Saved = %v, want web's variables unexpanded", got.Saved)
	}
	if n := vault.readCount("db"); n != 0 {
		t.Errorf("read the variables of an unreferenced service %d times", n)
	}

	vault.set("web", map[string]string{"P1": "${{ db.PASSWORD }}", "P2": "${{ db.URL }}", "PORT": "9999"})
	if got, err = f.d.ResolveVariables(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if got.Env["P1"] != "vault-db-secret" || got.Env["P2"] != "pg://db" || got.Env["PORT"] != "9999" {
		t.Errorf("Env = %v; want db's values and web's own PORT over the injected one", got.Env)
	}
	if n := vault.readCount("db"); n != 1 {
		t.Errorf("read db %d times for two references, want 1", n)
	}
}

func TestDeployReadsEachServiceOnce(t *testing.T) {
	f, vault := newVaultFixture(t)
	vault.set("web", map[string]string{"SECRET": "vault-web-secret", "DB": "${{ db.PASSWORD }}"})
	// Port detection resolves the variables again.
	f.svc.Port = 0
	if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
		t.Fatal(err)
	}
	f.docker.exposed = []int{3000}
	f.docker.output = "secret vault-web-secret and vault-db-secret\n"

	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
	}
	for _, name := range []string{"web", "db"} {
		if n := vault.readCount(name); n != 1 {
			t.Errorf("read %s %d times in one deployment, want 1", name, n)
		}
	}
	env := strings.Join(f.docker.runs[0].Env, "\n")
	for _, want := range []string{"SECRET=vault-web-secret", "DB=vault-db-secret", "PORT=3000"} {
		if !strings.Contains(env, want) {
			t.Errorf("env %q lacks %s", env, want)
		}
	}
	if want := []string{"DB", "SECRET"}; dep.Runtime == nil || !slices.Equal(dep.Runtime.SecretKeys, want) {
		t.Errorf("runtime = %+v, want secret keys %q", dep.Runtime, want)
	}
	assertLog(t, f.buildLog(t, dep.ID), []string{"secret *** and ***"}, []string{"vault-web-secret", "vault-db-secret"})

	// Restarting and recreating the container use the recorded runtime.
	ctx := context.Background()
	if err := f.d.RestartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.d.StopService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	f.docker.Remove(ctx, dep.ContainerID)
	if err := f.d.StartService(ctx, f.svc.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.BuildLog(ctx, dep.ID, 100); err != nil {
		t.Fatal(err)
	}
	if n := vault.readCount("web"); n != 1 {
		t.Errorf("read web %d times in all, want restarts, recreation, and log masking not to read", n)
	}
}

func TestDeployFailsWhenVariablesCannotBeRead(t *testing.T) {
	f, vault := newVaultFixture(t)
	vault.err = errors.New("variables of service web: onepassword: 1Password request failed: rate limited")
	dep := f.wait(t, f.deploy(t).ID, terminal)
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, "1Password") {
		t.Fatalf("deployment = %s (%q), want failed with the 1Password error", dep.Status, dep.Error)
	}
	if len(f.docker.runs) != 0 {
		t.Error("started a container without its variables")
	}
}
