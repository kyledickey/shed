// Package onepassword keeps variables in 1Password vault items with a service
// account token, through the official 1Password Go SDK.
package onepassword

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"

	sdk "github.com/1password/onepassword-sdk-go"
)

var (
	// ErrInvalidToken is wrapped by errors for a token 1Password rejects.
	ErrInvalidToken = errors.New("onepassword: 1Password rejected the token")
	// ErrItemMissing is wrapped by errors for an item that is not in its
	// vault.
	ErrItemMissing = errors.New("onepassword: 1Password item is missing")
	// ErrRequest is wrapped by errors for a request 1Password fails.
	ErrRequest = errors.New("onepassword: 1Password request failed")
)

// MissingItemError reports an item that is not in its vault. It matches
// ErrItemMissing.
type MissingItemError struct{ ItemID string }

func (e *MissingItemError) Error() string { return ErrItemMissing.Error() + ": " + e.ItemID }

// Is reports whether target is ErrItemMissing.
func (e *MissingItemError) Is(target error) bool { return target == ErrItemMissing }

// SectionTitle is the title of the item section that holds the variables.
const SectionTitle = "Variables"

// variableName matches the field titles that are variables.
var variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Item is an item that holds variables, each a concealed field titled with
// its key in the section titled [SectionTitle].
type Item struct {
	Title     string
	Tags      []string
	Variables map[string]string
}

// api is the part of the SDK Client uses. sdkAPI implements it.
type api interface {
	ListVaults(ctx context.Context) ([]sdk.VaultOverview, error)
	CreateVault(ctx context.Context, title string) (sdk.Vault, error)
	CreateItem(ctx context.Context, params sdk.ItemCreateParams) (sdk.Item, error)
	GetItems(ctx context.Context, vaultID string, itemIDs []string) (sdk.ItemsGetAllResponse, error)
	PutItem(ctx context.Context, item sdk.Item) (sdk.Item, error)
	ArchiveItem(ctx context.Context, vaultID, itemID string) error
	GetVariables(ctx context.Context, environmentID string) (sdk.GetVariablesResponse, error)
}

// Client reads and writes 1Password vaults and items, each call with the
// service account token given for it. It is safe for concurrent use.
//
// The SDK runs its core as WebAssembly in-process. Loading it takes about a
// second and about 110 MB of memory, which the SDK keeps for the life of the
// process. Client signs in once per token and reuses that SDK client until a
// request with it fails.
type Client struct {
	version string
	log     *slog.Logger
	open    func(ctx context.Context, token string) (api, error)

	mu      sync.Mutex
	clients map[string]api // by token
}

// New returns a Client that reports version as shed's version to 1Password.
func New(version string, log *slog.Logger) *Client {
	c := &Client{version: version, log: log, clients: make(map[string]api)}
	c.open = c.openSDK
	return c
}

func (c *Client) openSDK(ctx context.Context, token string) (api, error) {
	cl, err := sdk.NewClient(ctx,
		sdk.WithServiceAccountToken(token),
		sdk.WithIntegrationInfo("shed", c.version))
	if err != nil {
		return nil, err
	}
	return sdkAPI{cl}, nil
}

// sdkAPI holds the SDK client itself, not just its APIs: the SDK ends the
// client's session in the core when the *sdk.Client is garbage collected,
// after which requests fail with "invalid client id".
type sdkAPI struct{ client *sdk.Client }

func (a sdkAPI) ListVaults(ctx context.Context) ([]sdk.VaultOverview, error) {
	return a.client.Vaults().List(ctx)
}

func (a sdkAPI) CreateVault(ctx context.Context, title string) (sdk.Vault, error) {
	return a.client.Vaults().Create(ctx, sdk.VaultCreateParams{Title: title})
}

func (a sdkAPI) CreateItem(ctx context.Context, params sdk.ItemCreateParams) (sdk.Item, error) {
	return a.client.Items().Create(ctx, params)
}

func (a sdkAPI) GetItems(ctx context.Context, vaultID string, itemIDs []string) (sdk.ItemsGetAllResponse, error) {
	return a.client.Items().GetAll(ctx, vaultID, itemIDs)
}

func (a sdkAPI) PutItem(ctx context.Context, item sdk.Item) (sdk.Item, error) {
	return a.client.Items().Put(ctx, item)
}

func (a sdkAPI) ArchiveItem(ctx context.Context, vaultID, itemID string) error {
	return a.client.Items().Archive(ctx, vaultID, itemID)
}

func (a sdkAPI) GetVariables(ctx context.Context, environmentID string) (sdk.GetVariablesResponse, error) {
	return a.client.Environments().GetVariables(ctx, environmentID)
}

// Preload signs in with token in the background, which loads the SDK core,
// so the first request does not wait for it. A failure is logged; the next
// request tries again.
func (c *Client) Preload(ctx context.Context, token string) {
	go func() {
		if _, err := c.client(ctx, token); err != nil {
			c.log.Warn("load 1Password", "err", err)
		}
	}()
}

// SignIn signs in with token. It returns an error wrapping ErrInvalidToken
// if 1Password rejects the token.
func (c *Client) SignIn(ctx context.Context, token string) error {
	_, err := c.client(ctx, token)
	return err
}

// Vault returns the ID of the vault titled name that token can access,
// creating the vault if there is none.
func (c *Client) Vault(ctx context.Context, token, name string) (string, error) {
	a, err := c.client(ctx, token)
	if err != nil {
		return "", err
	}
	vaults, err := a.ListVaults(ctx)
	if err != nil {
		return "", c.failed(token, a, "list vaults", err)
	}
	for _, v := range vaults {
		if v.Title == name {
			return v.ID, nil
		}
	}
	v, err := a.CreateVault(ctx, name)
	if err != nil {
		return "", c.failed(token, a, "create vault "+name, err)
	}
	return v.ID, nil
}

// CreateItem creates item as a Secure Note in a vault and returns its ID.
func (c *Client) CreateItem(ctx context.Context, token, vaultID string, item Item) (string, error) {
	a, err := c.client(ctx, token)
	if err != nil {
		return "", err
	}
	section := sdk.ItemSection{ID: newID(), Title: SectionTitle}
	created, err := a.CreateItem(ctx, sdk.ItemCreateParams{
		Category: sdk.ItemCategorySecureNote,
		VaultID:  vaultID,
		Title:    item.Title,
		Tags:     item.Tags,
		Sections: []sdk.ItemSection{section},
		Fields:   variableFields(section.ID, item.Variables),
	})
	if err != nil {
		return "", c.failed(token, a, "create item "+item.Title, err)
	}
	return created.ID, nil
}

// Variables returns the variables of items in a vault by item ID: every
// field whose title is a variable name. If an item is not in the vault, the
// error is a *MissingItemError.
func (c *Client) Variables(ctx context.Context, token, vaultID string, itemIDs []string) (map[string]map[string]string, error) {
	a, err := c.client(ctx, token)
	if err != nil {
		return nil, err
	}
	items, err := c.items(ctx, token, a, vaultID, itemIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]string, len(items))
	for i, item := range items {
		vars := make(map[string]string)
		for _, f := range item.Fields {
			if variableName.MatchString(f.Title) {
				vars[f.Title] = f.Value
			}
		}
		out[itemIDs[i]] = vars
	}
	return out, nil
}

// SetVariables replaces the variables of an item and keeps its other fields.
// It returns an error wrapping ErrItemMissing if the item is not in the
// vault.
func (c *Client) SetVariables(ctx context.Context, token, vaultID, itemID string, vars map[string]string) error {
	return c.update(ctx, token, vaultID, itemID, func(item *sdk.Item) {
		item.Fields = slices.DeleteFunc(item.Fields, func(f sdk.ItemField) bool {
			return variableName.MatchString(f.Title)
		})
		i := slices.IndexFunc(item.Sections, func(s sdk.ItemSection) bool { return s.Title == SectionTitle })
		if i < 0 {
			item.Sections = append(item.Sections, sdk.ItemSection{ID: newID(), Title: SectionTitle})
			i = len(item.Sections) - 1
		}
		item.Fields = append(item.Fields, variableFields(item.Sections[i].ID, vars)...)
	})
}

// Retitle sets the title of an item and replaces its tags under the same
// root as tags (the part before the first "/"), keeping its other tags. It
// returns an error wrapping ErrItemMissing if the item is not in the vault.
func (c *Client) Retitle(ctx context.Context, token, vaultID, itemID, title string, tags []string) error {
	return c.update(ctx, token, vaultID, itemID, func(item *sdk.Item) {
		item.Title = title
		item.Tags = slices.DeleteFunc(item.Tags, func(t string) bool {
			return slices.ContainsFunc(tags, func(n string) bool { return sameRoot(t, n) })
		})
		item.Tags = append(item.Tags, tags...)
	})
}

// sameRoot reports whether two tags share their first path segment.
func sameRoot(a, b string) bool {
	ra, _, _ := strings.Cut(a, "/")
	rb, _, _ := strings.Cut(b, "/")
	return ra == rb
}

// Archive moves an item to the archive.
func (c *Client) Archive(ctx context.Context, token, vaultID, itemID string) error {
	a, err := c.client(ctx, token)
	if err != nil {
		return err
	}
	if err := a.ArchiveItem(ctx, vaultID, itemID); err != nil {
		return c.failed(token, a, "archive item "+itemID, err)
	}
	return nil
}

// EnvironmentVariables returns the variables of an Environment, read with
// token.
func (c *Client) EnvironmentVariables(ctx context.Context, token, environmentID string) (map[string]string, error) {
	a, err := c.client(ctx, token)
	if err != nil {
		return nil, err
	}
	resp, err := a.GetVariables(ctx, environmentID)
	if err != nil {
		return nil, c.failed(token, a, "read Environment "+environmentID, err)
	}
	vars := make(map[string]string, len(resp.Variables))
	for _, v := range resp.Variables {
		vars[v.Name] = v.Value
	}
	return vars, nil
}

// update reads an item, changes it with change, and writes it back.
func (c *Client) update(ctx context.Context, token, vaultID, itemID string, change func(*sdk.Item)) error {
	a, err := c.client(ctx, token)
	if err != nil {
		return err
	}
	items, err := c.items(ctx, token, a, vaultID, []string{itemID})
	if err != nil {
		return err
	}
	item := items[0]
	change(&item)
	if _, err := a.PutItem(ctx, item); err != nil {
		return c.failed(token, a, "write item "+itemID, err)
	}
	return nil
}

// items reads items of a vault, in the order of itemIDs.
func (c *Client) items(ctx context.Context, token string, a api, vaultID string, itemIDs []string) ([]sdk.Item, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	resp, err := a.GetItems(ctx, vaultID, itemIDs)
	if err != nil {
		return nil, c.failed(token, a, "read items", err)
	}
	if len(resp.IndividualResponses) != len(itemIDs) {
		return nil, fmt.Errorf("%w: read %d items, got %d", ErrRequest, len(itemIDs), len(resp.IndividualResponses))
	}
	items := make([]sdk.Item, len(itemIDs))
	for i, r := range resp.IndividualResponses {
		switch {
		case r.Error != nil && r.Error.Type == sdk.ItemsGetAllErrorTypeVariantItemNotFound:
			return nil, &MissingItemError{itemIDs[i]}
		case r.Error != nil:
			return nil, fmt.Errorf("%w: read item %s: %s", ErrRequest, itemIDs[i], r.Error.Type)
		case r.Content == nil:
			return nil, fmt.Errorf("%w: read item %s: empty response", ErrRequest, itemIDs[i])
		}
		items[i] = *r.Content
	}
	return items, nil
}

// variableFields returns vars as concealed fields of a section, sorted by
// key.
func variableFields(sectionID string, vars map[string]string) []sdk.ItemField {
	fields := make([]sdk.ItemField, 0, len(vars))
	for k, v := range vars {
		fields = append(fields, sdk.ItemField{
			ID:        newID(),
			Title:     k,
			SectionID: &sectionID,
			FieldType: sdk.ItemFieldTypeConcealed,
			Value:     v,
		})
	}
	slices.SortFunc(fields, func(a, b sdk.ItemField) int { return strings.Compare(a.Title, b.Title) })
	return fields
}

// newID returns a random ID for an item section or field, in the form
// 1Password uses.
func newID() string {
	return strings.ToLower(rand.Text())
}

// failed forgets a's SDK client for token, which may have lost its session,
// and returns err wrapped in ErrRequest.
func (c *Client) failed(token string, a api, what string, err error) error {
	c.mu.Lock()
	if c.clients[token] == a {
		delete(c.clients, token)
	}
	c.mu.Unlock()
	return fmt.Errorf("%w: %s: %w", ErrRequest, what, err)
}

// client returns the SDK client for token, signing in if needed.
func (c *Client) client(ctx context.Context, token string) (api, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.clients[token]; ok {
		return a, nil
	}
	a, err := c.open(ctx, token)
	var limited *sdk.RateLimitExceededError
	switch {
	case err == nil:
		c.clients[token] = a
		return a, nil
	case ctx.Err() != nil, errors.As(err, &limited):
		return nil, fmt.Errorf("%w: sign in: %w", ErrRequest, err)
	default:
		return nil, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
}
