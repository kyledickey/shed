package deploy

import (
	"context"
	"errors"
	"fmt"
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

// fakeOnePassword serves 1Password Environments, each to the token
// "<id>-token" only, and counts reads.
type fakeOnePassword struct {
	mu    sync.Mutex
	envs  map[string]map[string]string
	reads map[string]int
	err   error
}

func (f *fakeOnePassword) EnvironmentVariables(_ context.Context, token, id string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads[id]++
	if f.err != nil {
		return nil, f.err
	}
	if token != id+"-token" {
		return nil, fmt.Errorf("token %s cannot read Environment %s", token, id)
	}
	return maps.Clone(f.envs[id]), nil
}

func (f *fakeOnePassword) readCount(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[id]
}

// linkEnvironment links svc to the 1Password Environment id, read with token.
func (f *fixture) linkEnvironment(t *testing.T, svc store.Service, id, token string) {
	t.Helper()
	if err := f.st.SetOnePasswordLink(context.Background(), store.OnePasswordLink{ServiceID: svc.ID, Environment: id, Token: token}); err != nil {
		t.Fatal(err)
	}
}

// newOnePasswordFixture links web and a new service db to Environments.
func newOnePasswordFixture(t *testing.T) (*fixture, *fakeOnePassword, store.Service) {
	t.Helper()
	f := newFixture(t)
	op := &fakeOnePassword{
		envs: map[string]map[string]string{
			"webenv": {"FROM_OP": "op-web-secret", "GREETING": "op-greeting", "PORT": "9999", "NAME": "${{ SHED_SERVICE_NAME }}"},
			"dbenv":  {"URL": "pg://op", "PASSWORD": "op-db-secret"},
		},
		reads: make(map[string]int),
	}
	f.d.onePassword = op
	f.linkEnvironment(t, f.svc, "webenv", "webenv-token")
	db, err := f.st.CreateService(context.Background(), store.Service{ProjectID: f.svc.ProjectID, Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	f.linkEnvironment(t, db, "dbenv", "dbenv-token")
	return f, op, db
}

func TestResolveVariablesOnePassword(t *testing.T) {
	f, op, db := newOnePasswordFixture(t)
	ctx := context.Background()

	got, err := f.d.ResolveVariables(ctx, f.svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Injected < 1Password < own.
	want := map[string]string{"FROM_OP": "op-web-secret", "GREETING": "hi from web", "PORT": "9999", "NAME": "web"}
	for k, v := range want {
		if got.Env[k] != v {
			t.Errorf("%s = %q, want %q", k, got.Env[k], v)
		}
	}
	if want := []string{"FROM_OP", "NAME", "PORT"}; !slices.Equal(got.OnePassword, want) {
		t.Errorf("OnePassword = %q, want %q", got.OnePassword, want)
	}
	if n := op.readCount("dbenv"); n != 0 {
		t.Errorf("read the Environment of an unreferenced service %d times", n)
	}

	// A key db defines itself does not need its Environment.
	if err := f.st.SetVariables(ctx, db.ID, map[string]string{"URL": "pg://own"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"DB": "${{ db.URL }}"}); err != nil {
		t.Fatal(err)
	}
	if got, err = f.d.ResolveVariables(ctx, f.svc.ID); err != nil || got.Env["DB"] != "pg://own" {
		t.Fatalf("DB = %q, %v; want pg://own", got.Env["DB"], err)
	}
	if n := op.readCount("dbenv"); n != 0 {
		t.Errorf("read db's Environment %d times for a key db defines", n)
	}

	// A key only db's Environment has is read from it, once.
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{
		"P1": "${{ db.PASSWORD }}", "P2": "${{ db.PASSWORD }}",
	}); err != nil {
		t.Fatal(err)
	}
	if got, err = f.d.ResolveVariables(ctx, f.svc.ID); err != nil || got.Env["P1"] != "op-db-secret" || got.Env["P2"] != "op-db-secret" {
		t.Fatalf("P1, P2 = %q, %q, %v; want op-db-secret", got.Env["P1"], got.Env["P2"], err)
	}
	if n := op.readCount("dbenv"); n != 1 {
		t.Errorf("read db's Environment %d times, want 1", n)
	}
	if slices.Contains(got.OnePassword, "P1") {
		t.Errorf("OnePassword = %q lists an own variable", got.OnePassword)
	}
}

func TestDeployOnePassword(t *testing.T) {
	f, op, _ := newOnePasswordFixture(t)
	if err := f.st.SetVariables(context.Background(), f.svc.ID, map[string]string{"DB": "${{ db.PASSWORD }}"}); err != nil {
		t.Fatal(err)
	}
	// Port detection resolves the variables again.
	f.svc.Port = 0
	if err := f.st.UpdateService(context.Background(), f.svc); err != nil {
		t.Fatal(err)
	}
	f.docker.exposed = []int{3000}
	f.docker.output = "secret op-web-secret and op-db-secret\n"

	dep := f.wait(t, f.deploy(t).ID, terminal)
	f.settle(t)
	if dep.Status != store.StatusActive {
		t.Fatalf("status = %s (%q), want active", dep.Status, dep.Error)
	}
	for _, id := range []string{"webenv", "dbenv"} {
		if n := op.readCount(id); n != 1 {
			t.Errorf("read %s %d times in one deployment, want 1", id, n)
		}
	}
	env := strings.Join(f.docker.runs[0].Env, "\n")
	for _, want := range []string{"FROM_OP=op-web-secret", "DB=op-db-secret", "PORT=9999"} {
		if !strings.Contains(env, want) {
			t.Errorf("env %q lacks %s", env, want)
		}
	}
	if want := []string{"DB", "FROM_OP", "GREETING", "NAME", "PORT"}; dep.Runtime == nil || !slices.Equal(dep.Runtime.SecretKeys, want) {
		t.Errorf("runtime = %+v, want secret keys %q", dep.Runtime, want)
	}
	log := f.buildLog(t, dep.ID)
	assertLog(t, log, []string{"==> Reading 1Password Environment webenv", "4 variables", "secret *** and ***"}, []string{"op-web-secret", "op-db-secret"})
}

func TestDeployFailsWhenOnePasswordFails(t *testing.T) {
	f, op, _ := newOnePasswordFixture(t)
	op.err = errors.New("onepassword: 1Password rejected the token")
	dep := f.wait(t, f.deploy(t).ID, terminal)
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, "1Password") {
		t.Fatalf("deployment = %s (%q), want failed with a 1Password error", dep.Status, dep.Error)
	}
	if len(f.docker.runs) != 0 {
		t.Error("started a container without its 1Password variables")
	}
}

func TestDeployFailsWithoutOnePasswordToken(t *testing.T) {
	f, op, _ := newOnePasswordFixture(t)
	f.linkEnvironment(t, f.svc, "webenv", "")
	dep := f.wait(t, f.deploy(t).ID, terminal)
	want := "1Password link of service web has no token; add one on its Variables tab"
	if dep.Status != store.StatusFailed || !strings.Contains(dep.Error, want) {
		t.Fatalf("deployment = %s (%q), want failed with %q", dep.Status, dep.Error, want)
	}
	if len(f.docker.runs) != 0 || op.readCount("webenv") != 0 {
		t.Error("deployed or read 1Password without a token")
	}
	if _, err := f.d.ResolveVariables(context.Background(), f.svc.ID); !errors.Is(err, ErrNoOnePasswordToken) {
		t.Errorf("ResolveVariables() error = %v, want ErrNoOnePasswordToken", err)
	}
}

func TestResolveVariablesTokenlessOtherService(t *testing.T) {
	f, op, db := newOnePasswordFixture(t)
	ctx := context.Background()
	f.linkEnvironment(t, db, "dbenv", "")
	if err := f.st.SetVariables(ctx, db.ID, map[string]string{"URL": "pg://own"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"DB": "${{ db.URL }}"}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.d.ResolveVariables(ctx, f.svc.ID); err != nil || got.Env["DB"] != "pg://own" {
		t.Fatalf("DB = %q, %v; a key db defines needs no token", got.Env["DB"], err)
	}
	if err := f.st.SetVariables(ctx, f.svc.ID, map[string]string{"DB": "${{ db.PASSWORD }}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.d.ResolveVariables(ctx, f.svc.ID); !errors.Is(err, ErrNoOnePasswordToken) || !strings.Contains(err.Error(), "service db") {
		t.Errorf("ResolveVariables() error = %v, want db's missing token", err)
	}
	if n := op.readCount("dbenv"); n != 0 {
		t.Errorf("read db's Environment %d times without a token", n)
	}
}
