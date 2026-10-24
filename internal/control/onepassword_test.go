package control

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/variables"
)

// fakeOnePassword is a 1Password account that accepts the token "tok".
type fakeOnePassword struct {
	mu      sync.Mutex
	items   map[string]onepassword.Item
	created int
	// err, if set, fails every request but signing in.
	err error
}

func newFakeOnePassword() *fakeOnePassword {
	return &fakeOnePassword{items: make(map[string]onepassword.Item)}
}

func (f *fakeOnePassword) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeOnePassword) item(id string) (onepassword.Item, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	return item, ok
}

func (f *fakeOnePassword) Preload(context.Context, string) {}

func (f *fakeOnePassword) SignIn(_ context.Context, token string) error {
	if token != "tok" {
		return fmt.Errorf("%w: invalid service account token", onepassword.ErrInvalidToken)
	}
	return nil
}

func (f *fakeOnePassword) Vault(context.Context, string, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return "vault1", f.err
}

func (f *fakeOnePassword) CreateItem(_ context.Context, _, _ string, item onepassword.Item) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	f.created++
	id := fmt.Sprintf("item%d", f.created)
	item.Variables = maps.Clone(item.Variables)
	f.items[id] = item
	return id, nil
}

func (f *fakeOnePassword) Variables(_ context.Context, _, _ string, ids []string) (map[string]map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]map[string]string)
	for _, id := range ids {
		item, ok := f.items[id]
		if !ok {
			return nil, &onepassword.MissingItemError{ItemID: id}
		}
		out[id] = maps.Clone(item.Variables)
	}
	return out, nil
}

func (f *fakeOnePassword) SetVariables(_ context.Context, _, _, id string, vars map[string]string) error {
	return f.update(id, func(item *onepassword.Item) { item.Variables = maps.Clone(vars) })
}

func (f *fakeOnePassword) Retitle(_ context.Context, _, _, id, title string, tags []string) error {
	return f.update(id, func(item *onepassword.Item) { item.Title, item.Tags = title, tags })
}

func (f *fakeOnePassword) update(id string, change func(*onepassword.Item)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	item, ok := f.items[id]
	if !ok {
		return &onepassword.MissingItemError{ItemID: id}
	}
	change(&item)
	f.items[id] = item
	return nil
}

func (f *fakeOnePassword) Archive(_ context.Context, _, _, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.items, id)
	return nil
}

var errRateLimited = fmt.Errorf("%w: rate limited", onepassword.ErrRequest)

// connect connects 1Password with the fixture's fake account.
func (f *fixture) connect(t *testing.T) {
	t.Helper()
	if _, err := f.plane.ConnectOnePassword(context.Background(), "tok"); err != nil {
		t.Fatal(err)
	}
}

// itemOf returns the 1Password item of a service.
func (f *fixture) itemOf(t *testing.T, serviceID string) onepassword.Item {
	t.Helper()
	id, err := f.st.OnePasswordItem(context.Background(), serviceID)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := f.op.item(id)
	if !ok {
		t.Fatalf("item %s of service %s is not in 1Password", id, serviceID)
	}
	return item
}

func TestConnectOnePassword(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	if err := f.st.SetVariables(ctx, svc.ID, map[string]string{"API_KEY": "k"}); err != nil {
		t.Fatal(err)
	}

	_, err := f.plane.ConnectOnePassword(ctx, "  ")
	wantKind(t, err, ErrInvalid, "token is required")
	_, err = f.plane.ConnectOnePassword(ctx, "wrong")
	wantKind(t, err, ErrInvalid, "1Password rejected the token")
	f.op.fail(errRateLimited)
	_, err = f.plane.ConnectOnePassword(ctx, "tok")
	wantKind(t, err, ErrUpstream, "rate limited")
	if got, _ := f.st.Variables(ctx, svc.ID); got["API_KEY"] != "k" {
		t.Fatalf("variables after failed connects = %v, want them kept in shed.db", got)
	}
	f.op.fail(nil)

	got, err := f.plane.ConnectOnePassword(ctx, " tok ")
	if err != nil || got != (variables.Status{Connected: true, Vault: "shed", Items: 1}) {
		t.Fatalf("ConnectOnePassword() = %+v, %v", got, err)
	}
	if item := f.itemOf(t, svc.ID); item.Variables["API_KEY"] != "k" {
		t.Errorf("item = %+v, want the service's variables", item)
	}
	if got, _ := f.st.Variables(ctx, svc.ID); len(got) != 0 {
		t.Errorf("shed.db still has %v", got)
	}
	if token, _, err := f.st.OnePassword(ctx); err != nil || token != "tok" {
		t.Errorf("saved token = %q, %v; want it trimmed", token, err)
	}
	_, err = f.plane.ConnectOnePassword(ctx, "tok")
	wantKind(t, err, ErrConflict, "already connected")
	if got, err := f.plane.OnePassword(ctx); err != nil || !got.Connected {
		t.Errorf("OnePassword() = %+v, %v", got, err)
	}
}

func TestVariablesWhileConnected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	f.connect(t)

	got, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"A": "1"})
	if err != nil || !maps.Equal(got, map[string]string{"A": "1"}) {
		t.Fatalf("SetVariables() = %v, %v", got, err)
	}
	if saved, _ := f.st.Variables(ctx, svc.ID); len(saved) != 0 {
		t.Errorf("SetVariables wrote shed.db while connected: %v", saved)
	}
	if got, err := f.plane.Variables(ctx, svc.ID); err != nil || got["A"] != "1" {
		t.Errorf("Variables() = %v, %v; want the item's", got, err)
	}

	f.op.fail(errRateLimited)
	_, err = f.plane.Variables(ctx, svc.ID)
	wantKind(t, err, ErrUpstream, "1Password")
	f.op.fail(nil)

	id, _ := f.st.OnePasswordItem(ctx, svc.ID)
	if err := f.op.Archive(ctx, "", "", id); err != nil {
		t.Fatal(err)
	}
	_, err = f.plane.Variables(ctx, svc.ID)
	wantKind(t, err, ErrConflict, "the 1Password item of service web is missing")
	if _, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"B": "2"}); err != nil {
		t.Fatal(err)
	}
	if item := f.itemOf(t, svc.ID); !maps.Equal(item.Variables, map[string]string{"B": "2"}) {
		t.Errorf("recreated item = %+v", item)
	}
}

func TestCreateServiceWhileConnected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	project, err := f.plane.CreateProject(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	f.connect(t)

	db, err := f.plane.CreateService(ctx, project.ID, NewService{Name: "db", Kind: "postgres"})
	if err != nil {
		t.Fatal(err)
	}
	item := f.itemOf(t, db.ID)
	if item.Title != "shop / db" || !slices.Equal(item.Tags, []string{"shed/shop"}) || item.Variables["POSTGRES_PASSWORD"] == "" {
		t.Errorf("item = %+v, want shop / db with the template's variables", item)
	}

	f.op.fail(errRateLimited)
	_, err = f.plane.CreateService(ctx, project.ID, NewService{Name: "cache", Kind: "redis"})
	wantKind(t, err, ErrUpstream, "rate limited")
	services, _ := f.st.Services(ctx, project.ID)
	if len(services) != 1 {
		t.Errorf("services after a failed create = %d, want the service not created", len(services))
	}
}

func TestRenameProjectWhileConnected(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	f.connect(t)

	if _, err := f.plane.RenameProject(ctx, svc.ProjectID, "shop"); err != nil {
		t.Fatal(err)
	}
	if item := f.itemOf(t, svc.ID); item.Title != "shop / web" || !slices.Equal(item.Tags, []string{"shed/shop"}) {
		t.Errorf("item = %q tagged %v, want shop / web", item.Title, item.Tags)
	}

	f.op.fail(errRateLimited)
	_, err := f.plane.RenameProject(ctx, svc.ProjectID, "store")
	wantKind(t, err, ErrUpstream, "rate limited")
	if p, _ := f.st.Project(ctx, svc.ProjectID); p.Name != "shop" {
		t.Errorf("project name = %q after a failed rename, want shop", p.Name)
	}
}

func TestDeleteServiceArchivesItem(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	web := createApp(t, f.st)
	api, err := f.st.CreateService(ctx, store.Service{ProjectID: web.ProjectID, Name: "api", Kind: "app", Image: "nginx"})
	if err != nil {
		t.Fatal(err)
	}
	f.connect(t)
	id, _ := f.st.OnePasswordItem(ctx, web.ID)

	if err := f.plane.DeleteService(ctx, web.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.op.item(id); ok {
		t.Error("the deleted service's item is still active")
	}

	f.op.fail(errRateLimited)
	if err := f.plane.DeleteService(ctx, api.ID); err != nil {
		t.Errorf("DeleteService() with 1Password failing = %v, want the deletion to go ahead", err)
	}
}

func TestDisconnectOnePassword(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	svc := createApp(t, f.st)
	f.connect(t)
	if _, err := f.plane.SetVariables(ctx, svc.ID, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}

	f.op.fail(errRateLimited)
	_, err := f.plane.DisconnectOnePassword(ctx)
	wantKind(t, err, ErrUpstream, "rate limited")
	if s, _ := f.plane.OnePassword(ctx); !s.Connected {
		t.Error("a failed disconnect disconnected")
	}
	f.op.fail(nil)

	got, err := f.plane.DisconnectOnePassword(ctx)
	if err != nil || got != (variables.Status{}) {
		t.Fatalf("DisconnectOnePassword() = %+v, %v", got, err)
	}
	if saved, _ := f.st.Variables(ctx, svc.ID); !maps.Equal(saved, map[string]string{"A": "1"}) {
		t.Errorf("shed.db after disconnecting = %v", saved)
	}
	if _, err := f.st.OnePasswordItem(ctx, svc.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("item record after disconnecting: %v", err)
	}
}
