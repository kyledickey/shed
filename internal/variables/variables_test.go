package variables

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/store"
)

type fakeStore struct {
	projects []store.Project
	services []store.Service
	vars     map[string]map[string]string
	items    map[string]string
	token    string
	vault    string
	name     string
}

func (s *fakeStore) Project(_ context.Context, id string) (store.Project, error) {
	for _, p := range s.projects {
		if p.ID == id {
			return p, nil
		}
	}
	return store.Project{}, store.ErrNotFound
}

func (s *fakeStore) Projects(context.Context) ([]store.Project, error) { return s.projects, nil }

func (s *fakeStore) AllServices(context.Context) ([]store.Service, error) { return s.services, nil }

func (s *fakeStore) Variables(_ context.Context, id string) (map[string]string, error) {
	return maps.Clone(s.vars[id]), nil
}

func (s *fakeStore) SetVariables(_ context.Context, id string, vars map[string]string) error {
	s.vars[id] = maps.Clone(vars)
	return nil
}

func (s *fakeStore) OnePassword(context.Context) (store.OnePasswordConnection, error) {
	if s.token == "" {
		return store.OnePasswordConnection{}, store.ErrNotFound
	}
	return store.OnePasswordConnection{Token: s.token, VaultID: s.vault, VaultName: s.name}, nil
}

func (s *fakeStore) ConnectOnePassword(_ context.Context, c store.OnePasswordConnection) error {
	clear(s.vars)
	s.token, s.vault, s.name = c.Token, c.VaultID, c.VaultName
	return nil
}

func (s *fakeStore) DisconnectOnePassword(_ context.Context, vars map[string]map[string]string) error {
	for id, v := range vars {
		s.vars[id] = maps.Clone(v)
	}
	s.token, s.vault, s.name = "", "", ""
	clear(s.items)
	return nil
}

func (s *fakeStore) OnePasswordItem(_ context.Context, id string) (string, error) {
	if item, ok := s.items[id]; ok {
		return item, nil
	}
	return "", store.ErrNotFound
}

func (s *fakeStore) OnePasswordItems(context.Context) (map[string]string, error) {
	return maps.Clone(s.items), nil
}

func (s *fakeStore) SetOnePasswordItem(_ context.Context, id, item string) error {
	s.items[id] = item
	return nil
}

// fakeOnePassword is a 1Password account that accepts the token "tok",
// which can access vaults.
type fakeOnePassword struct {
	vaults  []onepassword.Vault
	items   map[string]onepassword.Item
	created int
	reads   int
	// failCreate fails the creation of the item with this title.
	failCreate string
	readErr    error
}

func newOnePassword() *fakeOnePassword {
	return &fakeOnePassword{
		vaults: []onepassword.Vault{{ID: "v1", Name: "Infra"}},
		items:  make(map[string]onepassword.Item),
	}
}

func (f *fakeOnePassword) Preload(context.Context, string) {}

func (f *fakeOnePassword) SignIn(_ context.Context, token string) error {
	if token != "tok" {
		return fmt.Errorf("%w: invalid token", onepassword.ErrInvalidToken)
	}
	return nil
}

func (f *fakeOnePassword) Vaults(ctx context.Context, token string) ([]onepassword.Vault, error) {
	if err := f.SignIn(ctx, token); err != nil {
		return nil, err
	}
	return f.vaults, nil
}

func (f *fakeOnePassword) CreateItem(_ context.Context, _, _ string, item onepassword.Item) (string, error) {
	if item.Title == f.failCreate {
		return "", fmt.Errorf("%w: rate limited", onepassword.ErrRequest)
	}
	f.created++
	id := fmt.Sprintf("item%d", f.created)
	item.Variables = maps.Clone(item.Variables)
	f.items[id] = item
	return id, nil
}

func (f *fakeOnePassword) Variables(_ context.Context, _, _ string, ids []string) (map[string]map[string]string, error) {
	f.reads++
	if f.readErr != nil {
		return nil, f.readErr
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
	item, ok := f.items[id]
	if !ok {
		return &onepassword.MissingItemError{ItemID: id}
	}
	item.Variables = maps.Clone(vars)
	f.items[id] = item
	return nil
}

func (f *fakeOnePassword) Retitle(_ context.Context, _, _, id, title string, tags []string) error {
	item, ok := f.items[id]
	if !ok {
		return &onepassword.MissingItemError{ItemID: id}
	}
	item.Title, item.Tags = title, tags
	f.items[id] = item
	return nil
}

func (f *fakeOnePassword) Archive(_ context.Context, _, _, id string) error {
	delete(f.items, id)
	return nil
}

var (
	shop = store.Project{ID: "p1", Name: "shop"}
	web  = store.Service{ID: "s1", ProjectID: "p1", Name: "web"}
	db   = store.Service{ID: "s2", ProjectID: "p1", Name: "db"}
)

func newKeeper() (*Keeper, *fakeStore, *fakeOnePassword) {
	st := &fakeStore{
		projects: []store.Project{shop},
		services: []store.Service{web, db},
		vars: map[string]map[string]string{
			web.ID: {"API": "${{ db.URL }}"},
			db.ID:  {"URL": "postgres://db", "PASSWORD": "secret"},
		},
		items: make(map[string]string),
	}
	op := newOnePassword()
	return New(st, op), st, op
}

func connect(t *testing.T, k *Keeper) {
	t.Helper()
	if err := k.Connect(context.Background(), "tok", ""); err != nil {
		t.Fatal(err)
	}
}

func TestConnectMovesVariablesToItems(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)

	item := op.items[st.items[db.ID]]
	if item.Title != "shop / db" || !slices.Equal(item.Tags, []string{"shed/shop"}) {
		t.Errorf("item of db = %q tagged %v, want shop / db tagged shed/shop", item.Title, item.Tags)
	}
	if want := map[string]string{"URL": "postgres://db", "PASSWORD": "secret"}; !maps.Equal(item.Variables, want) {
		t.Errorf("item variables = %v, want %v", item.Variables, want)
	}
	if len(st.vars) != 0 {
		t.Errorf("shed.db still has variables %v", st.vars)
	}
	if st.token != "tok" || st.vault != "v1" {
		t.Errorf("connection = %q, %q; want the token and its only vault", st.token, st.vault)
	}
	got, err := k.Status(ctx)
	if err != nil || got != (Status{Connected: true, Vault: "Infra", Items: 2}) {
		t.Errorf("Status = %+v, %v", got, err)
	}
	if err := k.Connect(ctx, "tok", ""); !errors.Is(err, ErrConnected) {
		t.Errorf("Connect while connected = %v, want ErrConnected", err)
	}
}

func TestConnectRejectedToken(t *testing.T) {
	k, st, op := newKeeper()
	err := k.Connect(context.Background(), "wrong", "")
	if !errors.Is(err, onepassword.ErrInvalidToken) {
		t.Errorf("Connect = %v, want ErrInvalidToken", err)
	}
	if st.token != "" || len(op.items) != 0 || len(st.vars) != 2 {
		t.Errorf("a rejected token changed state: token %q, items %v, vars %v", st.token, op.items, st.vars)
	}
}

func TestConnectPicksVault(t *testing.T) {
	ctx := context.Background()
	several := []onepassword.Vault{{ID: "v1", Name: "Infra"}, {ID: "v2", Name: "shed"}}
	for _, tt := range []struct {
		name   string
		vaults []onepassword.Vault
		id     string
		want   string // vault ID connected to; "" for ErrVault
	}{
		{"only vault", several[:1], "", "v1"},
		{"chosen", several, "v2", "v2"},
		{"none", nil, "", ""},
		{"several, none chosen", several, "", ""},
		{"not accessible", several, "v3", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			k, st, op := newKeeper()
			op.vaults = tt.vaults
			err := k.Connect(ctx, "tok", tt.id)
			if tt.want == "" {
				if !errors.Is(err, ErrVault) || st.token != "" || len(op.items) != 0 {
					t.Errorf("Connect = %v with token %q, items %v; want ErrVault and nothing changed", err, st.token, op.items)
				}
				return
			}
			if err != nil || st.vault != tt.want {
				t.Errorf("Connect = %v to vault %q, want %q", err, st.vault, tt.want)
			}
		})
	}
}

func TestConnectConvergesAfterFailure(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	op.failCreate = "shop / db"
	if err := k.Connect(ctx, "tok", ""); !errors.Is(err, onepassword.ErrRequest) {
		t.Fatalf("Connect = %v, want the 1Password failure", err)
	}
	if st.token != "" || len(st.vars) != 2 || st.vars[db.ID]["PASSWORD"] != "secret" {
		t.Fatalf("after a failed Connect: token %q, vars %v; want shed.db unchanged", st.token, st.vars)
	}
	if s, _ := k.Status(ctx); s.Connected {
		t.Errorf("Status after a failed Connect = %+v, want not connected", s)
	}
	if v, err := k.Get(ctx, db); err != nil || v["PASSWORD"] != "secret" {
		t.Errorf("Get after a failed Connect = %v, %v; want shed.db's variables", v, err)
	}

	// The variables change before the retry, which must replace the fields
	// of the item created by the first attempt instead of making another.
	st.vars[web.ID] = map[string]string{"API": "v2"}
	op.failCreate = ""
	connect(t, k)
	if op.created != 2 || len(op.items) != 2 {
		t.Errorf("created %d items, have %v; want the first attempt's item reused", op.created, op.items)
	}
	if v := op.items[st.items[web.ID]].Variables; v["API"] != "v2" {
		t.Errorf("web item = %v, want the variables at the retry", v)
	}
	if len(st.vars) != 0 || st.token != "tok" {
		t.Errorf("after the retry: vars %v, token %q", st.vars, st.token)
	}
}

func TestConnectedReadsAndWritesItems(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)

	if err := k.Set(ctx, web, map[string]string{"API": "new"}); err != nil {
		t.Fatal(err)
	}
	if len(st.vars) != 0 {
		t.Errorf("Set while connected wrote shed.db: %v", st.vars)
	}
	got, err := k.Get(ctx, web)
	if err != nil || !maps.Equal(got, map[string]string{"API": "new"}) {
		t.Errorf("Get = %v, %v; want the item's variables", got, err)
	}

	// The item is deleted in 1Password.
	delete(op.items, st.items[web.ID])
	_, err = k.Get(ctx, web)
	if !errors.Is(err, onepassword.ErrItemMissing) || err.Error() != "the 1Password item of service web is missing" {
		t.Errorf("Get of a missing item = %v", err)
	}
	if err := k.Set(ctx, web, map[string]string{"API": "again"}); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(ctx, web); err != nil || got["API"] != "again" {
		t.Errorf("Get after Set recreated the item = %v, %v", got, err)
	}
	if title := op.items[st.items[web.ID]].Title; title != "shop / web" {
		t.Errorf("recreated item title = %q", title)
	}
}

func TestNewServiceGetsItem(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)
	api := store.Service{ID: "s3", ProjectID: "p1", Name: "api"}
	st.services = append(st.services, api)
	if _, err := k.Get(ctx, api); !errors.Is(err, onepassword.ErrItemMissing) {
		t.Errorf("Get of a service without an item = %v, want ErrItemMissing", err)
	}
	if err := k.Set(ctx, api, map[string]string{"K": "v"}); err != nil {
		t.Fatal(err)
	}
	if item := op.items[st.items[api.ID]]; item.Title != "shop / api" || item.Variables["K"] != "v" {
		t.Errorf("item of the new service = %+v", item)
	}
}

func TestRetitleAndArchive(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)
	renamed := store.Project{ID: shop.ID, Name: "store"}
	if err := k.Retitle(ctx, renamed, []store.Service{web, db}); err != nil {
		t.Fatal(err)
	}
	if item := op.items[st.items[web.ID]]; item.Title != "store / web" || !slices.Equal(item.Tags, []string{"shed/store"}) {
		t.Errorf("retitled item = %q tagged %v", item.Title, item.Tags)
	}
	if err := k.Archive(ctx, web); err != nil {
		t.Fatal(err)
	}
	if _, ok := op.items[st.items[web.ID]]; ok {
		t.Error("Archive left the item")
	}
}

func TestDisconnectCopiesVariablesBack(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)
	if err := k.Set(ctx, web, map[string]string{"API": "from 1password"}); err != nil {
		t.Fatal(err)
	}

	op.readErr = fmt.Errorf("%w: network down", onepassword.ErrRequest)
	if err := k.Disconnect(ctx); !errors.Is(err, onepassword.ErrRequest) {
		t.Fatalf("Disconnect = %v, want the read failure", err)
	}
	if st.token != "tok" || len(st.items) != 2 || len(st.vars) != 0 {
		t.Fatalf("a failed Disconnect changed state: token %q, items %v, vars %v", st.token, st.items, st.vars)
	}

	op.readErr = nil
	if err := k.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if st.token != "" || len(st.items) != 0 {
		t.Errorf("after Disconnect: token %q, items %v", st.token, st.items)
	}
	if st.vars[web.ID]["API"] != "from 1password" || st.vars[db.ID]["PASSWORD"] != "secret" {
		t.Errorf("shed.db after Disconnect = %v", st.vars)
	}
	if len(op.items) != 2 {
		t.Errorf("Disconnect removed items from 1Password: %v", op.items)
	}
	reads := op.reads
	if got, err := k.Get(ctx, web); err != nil || got["API"] != "from 1password" || op.reads != reads {
		t.Errorf("Get after Disconnect = %v, %v with %d new reads; want shed.db", got, err, op.reads-reads)
	}
}

func TestDisconnectNamesMissingItem(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	connect(t, k)
	delete(op.items, st.items[db.ID])
	err := k.Disconnect(ctx)
	if !errors.Is(err, onepassword.ErrItemMissing) || err.Error() != "the 1Password item of service db is missing" {
		t.Errorf("Disconnect = %v", err)
	}
	if st.token == "" {
		t.Error("a failed Disconnect forgot the token")
	}
}

func TestNotConnectedUsesStore(t *testing.T) {
	ctx := context.Background()
	k, st, op := newKeeper()
	if err := k.Set(ctx, web, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Get(ctx, web); err != nil || got["A"] != "1" || st.vars[web.ID]["A"] != "1" {
		t.Errorf("Get = %v, %v; store %v", got, err, st.vars)
	}
	if err := k.Retitle(ctx, shop, st.services); err != nil {
		t.Fatal(err)
	}
	if err := k.Archive(ctx, web); err != nil {
		t.Fatal(err)
	}
	if op.reads != 0 || op.created != 0 {
		t.Error("called 1Password while not connected")
	}
	if s, err := k.Status(ctx); err != nil || s != (Status{}) {
		t.Errorf("Status = %+v, %v", s, err)
	}
}
