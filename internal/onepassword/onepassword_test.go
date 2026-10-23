package onepassword

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"testing"

	sdk "github.com/1password/onepassword-sdk-go"
)

// fake1Password is one account: the tokens it accepts, its vaults, and their
// items.
type fake1Password struct {
	tokens map[string]bool
	vaults []sdk.VaultOverview
	items  map[string]sdk.Item // by item ID
	envs   map[string][]sdk.EnvironmentVariable
	opens  map[string]int
	err    error // fails every request
	nextID int
}

type fakeAPI struct{ f *fake1Password }

func newFake() *fake1Password {
	return &fake1Password{
		tokens: map[string]bool{"tok": true},
		items:  make(map[string]sdk.Item),
		envs:   map[string][]sdk.EnvironmentVariable{"env1": {{Name: "API_KEY", Value: "k"}}},
		opens:  make(map[string]int),
	}
}

func (f *fake1Password) open(_ context.Context, token string) (api, error) {
	f.opens[token]++
	if !f.tokens[token] {
		return nil, errors.New("invalid service account token")
	}
	return fakeAPI{f}, nil
}

func (f *fake1Password) id() string {
	f.nextID++
	return string(rune('a'+f.nextID-1)) + "id"
}

func (a fakeAPI) ListVaults(context.Context) ([]sdk.VaultOverview, error) {
	return a.f.vaults, a.f.err
}

func (a fakeAPI) CreateVault(_ context.Context, title string) (sdk.Vault, error) {
	if a.f.err != nil {
		return sdk.Vault{}, a.f.err
	}
	v := sdk.Vault{ID: a.f.id(), Title: title}
	a.f.vaults = append(a.f.vaults, sdk.VaultOverview{ID: v.ID, Title: title})
	return v, nil
}

func (a fakeAPI) CreateItem(_ context.Context, p sdk.ItemCreateParams) (sdk.Item, error) {
	if a.f.err != nil {
		return sdk.Item{}, a.f.err
	}
	item := sdk.Item{ID: a.f.id(), VaultID: p.VaultID, Title: p.Title, Category: p.Category,
		Tags: p.Tags, Sections: p.Sections, Fields: p.Fields}
	a.f.items[item.ID] = item
	return item, nil
}

func (a fakeAPI) GetItems(_ context.Context, vaultID string, ids []string) (sdk.ItemsGetAllResponse, error) {
	if a.f.err != nil {
		return sdk.ItemsGetAllResponse{}, a.f.err
	}
	var resp sdk.ItemsGetAllResponse
	for _, id := range ids {
		item, ok := a.f.items[id]
		if !ok || item.VaultID != vaultID {
			resp.IndividualResponses = append(resp.IndividualResponses, sdk.Response[sdk.Item, sdk.ItemsGetAllError]{
				Error: &sdk.ItemsGetAllError{Type: sdk.ItemsGetAllErrorTypeVariantItemNotFound},
			})
			continue
		}
		resp.IndividualResponses = append(resp.IndividualResponses, sdk.Response[sdk.Item, sdk.ItemsGetAllError]{Content: &item})
	}
	return resp, nil
}

func (a fakeAPI) PutItem(_ context.Context, item sdk.Item) (sdk.Item, error) {
	if a.f.err != nil {
		return sdk.Item{}, a.f.err
	}
	a.f.items[item.ID] = item
	return item, nil
}

func (a fakeAPI) ArchiveItem(_ context.Context, _, id string) error {
	if a.f.err != nil {
		return a.f.err
	}
	delete(a.f.items, id)
	return nil
}

func (a fakeAPI) GetVariables(_ context.Context, id string) (sdk.GetVariablesResponse, error) {
	return sdk.GetVariablesResponse{Variables: a.f.envs[id]}, a.f.err
}

func newClient() (*Client, *fake1Password) {
	fake := newFake()
	c := New("test", slog.New(slog.DiscardHandler))
	c.open = fake.open
	return c, fake
}

func TestSignIn(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	if err := c.SignIn(ctx, "revoked"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("SignIn(revoked) = %v, want ErrInvalidToken", err)
	}
	for range 2 {
		if err := c.SignIn(ctx, "tok"); err != nil {
			t.Fatal(err)
		}
	}
	if fake.opens["tok"] != 1 {
		t.Errorf("signed in %d times, want the SDK client reused", fake.opens["tok"])
	}
}

func TestVaultFindsOrCreates(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	fake.vaults = []sdk.VaultOverview{{ID: "other", Title: "Private"}}
	id, err := c.Vault(ctx, "tok", "shed")
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.Vault(ctx, "tok", "shed")
	if err != nil {
		t.Fatal(err)
	}
	if id == "other" || again != id || len(fake.vaults) != 2 {
		t.Errorf("Vault = %q then %q with vaults %v; want one shed vault created and then found", id, again, fake.vaults)
	}
}

func TestItemRoundTrip(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	vault, err := c.Vault(ctx, "tok", "shed")
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.CreateItem(ctx, "tok", vault, Item{
		Title: "app / web", Tags: []string{"shed/app"},
		Variables: map[string]string{"B": "2", "A": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item := fake.items[id]
	if item.Category != sdk.ItemCategorySecureNote || item.Title != "app / web" || !slices.Equal(item.Tags, []string{"shed/app"}) {
		t.Errorf("created item = %+v", item)
	}
	if len(item.Sections) != 1 || item.Sections[0].Title != "Variables" {
		t.Fatalf("sections = %+v, want one titled Variables", item.Sections)
	}
	for i, want := range []string{"A", "B"} {
		f := item.Fields[i]
		if f.Title != want || f.FieldType != sdk.ItemFieldTypeConcealed || f.SectionID == nil || *f.SectionID != item.Sections[0].ID {
			t.Errorf("field %d = %+v, want concealed %s in the Variables section", i, f, want)
		}
	}

	// Fields the user added in 1Password: one that is not a variable name,
	// and one that is, outside the section.
	item.Fields = append(item.Fields,
		sdk.ItemField{ID: "u1", Title: "a note", Value: "keep me"},
		sdk.ItemField{ID: "u2", Title: "EXTRA", Value: "x"},
	)
	fake.items[id] = item
	got, err := c.Variables(ctx, "tok", vault, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"A": "1", "B": "2", "EXTRA": "x"}; !maps.Equal(got[id], want) {
		t.Errorf("Variables = %v, want %v", got[id], want)
	}

	if err := c.SetVariables(ctx, "tok", vault, id, map[string]string{"C": "3"}); err != nil {
		t.Fatal(err)
	}
	got, err = c.Variables(ctx, "tok", vault, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"C": "3"}; !maps.Equal(got[id], want) {
		t.Errorf("Variables after SetVariables = %v, want %v", got[id], want)
	}
	if !slices.ContainsFunc(fake.items[id].Fields, func(f sdk.ItemField) bool { return f.Value == "keep me" }) {
		t.Errorf("SetVariables dropped a field that is not a variable: %+v", fake.items[id].Fields)
	}
	if n := len(fake.items[id].Sections); n != 1 {
		t.Errorf("SetVariables left %d sections, want the Variables section reused", n)
	}

	fake.items[id] = withTags(fake.items[id], "shed/app", "mine")
	if err := c.Retitle(ctx, "tok", vault, id, "api / web", []string{"shed/api"}); err != nil {
		t.Fatal(err)
	}
	if item := fake.items[id]; item.Title != "api / web" || !slices.Equal(item.Tags, []string{"mine", "shed/api"}) {
		t.Errorf("after Retitle title = %q, tags = %v; want api / web with [mine shed/api]", item.Title, item.Tags)
	}

	if err := c.Archive(ctx, "tok", vault, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Variables(ctx, "tok", vault, []string{id}); !errors.Is(err, ErrItemMissing) {
		t.Errorf("Variables of an archived item = %v, want ErrItemMissing", err)
	}
	if err := c.SetVariables(ctx, "tok", vault, id, nil); !errors.Is(err, ErrItemMissing) {
		t.Errorf("SetVariables of a missing item = %v, want ErrItemMissing", err)
	}
}

func withTags(item sdk.Item, tags ...string) sdk.Item {
	item.Tags = tags
	return item
}

func TestVariablesOfSeveralItems(t *testing.T) {
	ctx := context.Background()
	c, _ := newClient()
	a, _ := c.CreateItem(ctx, "tok", "v", Item{Variables: map[string]string{"A": "1"}})
	b, _ := c.CreateItem(ctx, "tok", "v", Item{Variables: map[string]string{"B": "2"}})
	got, err := c.Variables(ctx, "tok", "v", []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if got[a]["A"] != "1" || got[b]["B"] != "2" || len(got) != 2 {
		t.Errorf("Variables = %v", got)
	}
}

func TestFailedRequestRebuildsClient(t *testing.T) {
	ctx := context.Background()
	c, fake := newClient()
	if err := c.SignIn(ctx, "tok"); err != nil {
		t.Fatal(err)
	}
	fake.err = errors.New("network down")
	if _, err := c.Vault(ctx, "tok", "shed"); !errors.Is(err, ErrRequest) || errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Vault on a failed request = %v, want ErrRequest", err)
	}
	fake.err = nil
	if _, err := c.Vault(ctx, "tok", "shed"); err != nil {
		t.Fatal(err)
	}
	if fake.opens["tok"] != 2 {
		t.Errorf("signed in %d times, want the client rebuilt once after a failed request", fake.opens["tok"])
	}
}
