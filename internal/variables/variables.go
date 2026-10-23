// Package variables keeps the variables of services where they live: in
// shed.db, or, while 1Password is connected, in one item per service in the
// shed vault. Callers read and write variables through a [Keeper] and never
// ask which.
package variables

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/store"
)

// VaultName is the name of the vault that holds the items.
const VaultName = "shed"

// ErrConnected is returned when connecting while 1Password is connected.
var ErrConnected = errors.New("variables: 1Password is already connected")

// Store holds the variables while 1Password is not connected, the
// connection, and the item of each service. *store.Store implements it.
type Store interface {
	Project(ctx context.Context, id string) (store.Project, error)
	Projects(ctx context.Context) ([]store.Project, error)
	AllServices(ctx context.Context) ([]store.Service, error)
	Variables(ctx context.Context, serviceID string) (map[string]string, error)
	SetVariables(ctx context.Context, serviceID string, vars map[string]string) error

	// OnePassword returns the connection's token and vault ID, or
	// store.ErrNotFound while 1Password is not connected.
	OnePassword(ctx context.Context) (token, vaultID string, err error)
	// ConnectOnePassword deletes every service's variables and saves the
	// connection, atomically.
	ConnectOnePassword(ctx context.Context, token, vaultID string) error
	// DisconnectOnePassword saves the variables of services by service ID
	// and deletes the connection and every service's item, atomically.
	DisconnectOnePassword(ctx context.Context, vars map[string]map[string]string) error
	// OnePasswordItem returns the ID of a service's item, or
	// store.ErrNotFound if it has none.
	OnePasswordItem(ctx context.Context, serviceID string) (string, error)
	// OnePasswordItems returns the item IDs of services by service ID.
	OnePasswordItems(ctx context.Context) (map[string]string, error)
	SetOnePasswordItem(ctx context.Context, serviceID, itemID string) error
}

// OnePassword reads and writes vault items. *onepassword.Client implements
// it.
type OnePassword interface {
	Preload(ctx context.Context, token string)
	SignIn(ctx context.Context, token string) error
	Vault(ctx context.Context, token, name string) (string, error)
	CreateItem(ctx context.Context, token, vaultID string, item onepassword.Item) (string, error)
	Variables(ctx context.Context, token, vaultID string, itemIDs []string) (map[string]map[string]string, error)
	SetVariables(ctx context.Context, token, vaultID, itemID string, vars map[string]string) error
	Retitle(ctx context.Context, token, vaultID, itemID, title string, tags []string) error
	Archive(ctx context.Context, token, vaultID, itemID string) error
}

var _ OnePassword = (*onepassword.Client)(nil)

// Status describes the 1Password connection.
type Status struct {
	Connected bool
	// Vault is the name of the vault, empty while not connected.
	Vault string
	// Items is how many services have an item, zero while not connected.
	Items int
}

// missingItemError reports the service whose item is missing. It matches
// onepassword.ErrItemMissing.
type missingItemError struct{ service string }

func (e missingItemError) Error() string {
	return "the 1Password item of service " + e.service + " is missing"
}

func (missingItemError) Is(target error) bool { return target == onepassword.ErrItemMissing }

// Keeper reads and writes the variables of services. It is safe for
// concurrent use.
type Keeper struct {
	store Store
	op    OnePassword

	// mu orders reads and writes of variables with connecting and
	// disconnecting, which move every service's variables between shed.db
	// and 1Password: a read must not see shed.db emptied before the
	// connection is saved, and a write must not land on the side being
	// left. Reads share it; writes, connecting, and disconnecting hold it
	// alone, connecting for as long as 1Password takes.
	mu sync.RWMutex
}

// New returns a Keeper.
func New(st Store, op OnePassword) *Keeper {
	return &Keeper{store: st, op: op}
}

// connection is a 1Password connection; the zero value is none.
type connection struct{ token, vault string }

func (k *Keeper) connection(ctx context.Context) (connection, bool, error) {
	token, vault, err := k.store.OnePassword(ctx)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return connection{}, false, nil
	case err != nil:
		return connection{}, false, err
	}
	return connection{token, vault}, true, nil
}

// Preload loads 1Password in the background if it is connected, so that the
// first read does not wait for it.
func (k *Keeper) Preload(ctx context.Context) error {
	c, ok, err := k.connection(ctx)
	if err != nil || !ok {
		return err
	}
	k.op.Preload(ctx, c.token)
	return nil
}

// Status reports the 1Password connection without calling 1Password.
func (k *Keeper) Status(ctx context.Context) (Status, error) {
	_, ok, err := k.connection(ctx)
	if err != nil || !ok {
		return Status{}, err
	}
	items, err := k.store.OnePasswordItems(ctx)
	if err != nil {
		return Status{}, err
	}
	return Status{Connected: true, Vault: VaultName, Items: len(items)}, nil
}

// Get returns a service's variables, with references unexpanded. While
// 1Password is connected they are read from the service's item; if it has
// none, or 1Password no longer has it, the error matches
// onepassword.ErrItemMissing.
func (k *Keeper) Get(ctx context.Context, svc store.Service) (map[string]string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	c, ok, err := k.connection(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return k.store.Variables(ctx, svc.ID)
	}
	item, err := k.store.OnePasswordItem(ctx, svc.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, missingItemError{svc.Name}
	}
	if err != nil {
		return nil, err
	}
	vars, err := k.op.Variables(ctx, c.token, c.vault, []string{item})
	if errors.Is(err, onepassword.ErrItemMissing) {
		return nil, missingItemError{svc.Name}
	}
	if err != nil {
		return nil, fmt.Errorf("variables of service %s: %w", svc.Name, err)
	}
	return vars[item], nil
}

// Set replaces a service's variables. While 1Password is connected they are
// written to the service's item, which is created if the service has none
// or 1Password no longer has it; a new service gets its item this way.
func (k *Keeper) Set(ctx context.Context, svc store.Service, vars map[string]string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	c, ok, err := k.connection(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return k.store.SetVariables(ctx, svc.ID, vars)
	}
	switch item, err := k.store.OnePasswordItem(ctx, svc.ID); {
	case err == nil:
		err := k.op.SetVariables(ctx, c.token, c.vault, item, vars)
		if !errors.Is(err, onepassword.ErrItemMissing) {
			return err
		}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	project, err := k.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return err
	}
	return k.create(ctx, c, project, svc, vars)
}

// create creates the item of a service and records it.
func (k *Keeper) create(ctx context.Context, c connection, project store.Project, svc store.Service, vars map[string]string) error {
	title, tags := itemTitle(project, svc)
	item, err := k.op.CreateItem(ctx, c.token, c.vault, onepassword.Item{Title: title, Tags: tags, Variables: vars})
	if err != nil {
		return err
	}
	return k.store.SetOnePasswordItem(ctx, svc.ID, item)
}

// itemTitle returns the title and tags of a service's item.
func itemTitle(project store.Project, svc store.Service) (string, []string) {
	return project.Name + " / " + svc.Name, []string{"shed/" + project.Name}
}

// Retitle gives the items of services of a project the project's and
// services' current names. Services without an item are skipped.
func (k *Keeper) Retitle(ctx context.Context, project store.Project, services []store.Service) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	c, ok, err := k.connection(ctx)
	if err != nil || !ok {
		return err
	}
	items, err := k.store.OnePasswordItems(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		item, ok := items[svc.ID]
		if !ok {
			continue
		}
		title, tags := itemTitle(project, svc)
		err := k.op.Retitle(ctx, c.token, c.vault, item, title, tags)
		if err != nil && !errors.Is(err, onepassword.ErrItemMissing) {
			return err
		}
	}
	return nil
}

// Archive archives the item of a service that is being deleted, if it has
// one.
func (k *Keeper) Archive(ctx context.Context, svc store.Service) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	c, ok, err := k.connection(ctx)
	if err != nil || !ok {
		return err
	}
	item, err := k.store.OnePasswordItem(ctx, svc.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil
	case err != nil:
		return err
	}
	return k.op.Archive(ctx, c.token, c.vault, item)
}

// Connect moves every service's variables from shed.db to items in the shed
// vault, which it creates if token cannot access one, and keeps variables
// there from then on. Items are recorded as they are created, and a service
// that has one from an earlier attempt gets its fields replaced, so after a
// failure Connect can be retried; until it succeeds, shed.db keeps the
// variables. An error matching onepassword.ErrInvalidToken means 1Password
// rejected the token. Connecting while connected returns ErrConnected.
func (k *Keeper) Connect(ctx context.Context, token string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok, err := k.connection(ctx); err != nil {
		return err
	} else if ok {
		return ErrConnected
	}
	if err := k.op.SignIn(ctx, token); err != nil {
		return err
	}
	vault, err := k.op.Vault(ctx, token, VaultName)
	if err != nil {
		return err
	}
	c := connection{token, vault}
	projects, err := k.projects(ctx)
	if err != nil {
		return err
	}
	services, err := k.store.AllServices(ctx)
	if err != nil {
		return err
	}
	items, err := k.store.OnePasswordItems(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		vars, err := k.store.Variables(ctx, svc.ID)
		if err != nil {
			return err
		}
		if item, ok := items[svc.ID]; ok {
			err := k.op.SetVariables(ctx, token, vault, item, vars)
			if !errors.Is(err, onepassword.ErrItemMissing) {
				if err != nil {
					return err
				}
				continue
			}
		}
		if err := k.create(ctx, c, projects[svc.ProjectID], svc, vars); err != nil {
			return err
		}
	}
	return k.store.ConnectOnePassword(ctx, token, vault)
}

func (k *Keeper) projects(ctx context.Context) (map[string]store.Project, error) {
	list, err := k.store.Projects(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]store.Project, len(list))
	for _, p := range list {
		out[p.ID] = p
	}
	return out, nil
}

// Disconnect reads every service's item, saves the variables in shed.db,
// and forgets the token, the vault, and the items, which stay in 1Password.
// If a read fails, nothing changes. Disconnecting while not connected
// changes nothing.
func (k *Keeper) Disconnect(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	c, ok, err := k.connection(ctx)
	if err != nil || !ok {
		return err
	}
	items, err := k.store.OnePasswordItems(ctx)
	if err != nil {
		return err
	}
	services, err := k.store.AllServices(ctx)
	if err != nil {
		return err
	}
	var ids []string
	for _, svc := range services {
		if item, ok := items[svc.ID]; ok {
			ids = append(ids, item)
		}
	}
	read, err := k.op.Variables(ctx, c.token, c.vault, ids)
	if miss := (*onepassword.MissingItemError)(nil); errors.As(err, &miss) {
		for _, svc := range services {
			if items[svc.ID] == miss.ItemID {
				return missingItemError{svc.Name}
			}
		}
	}
	if err != nil {
		return err
	}
	vars := make(map[string]map[string]string, len(services))
	for _, svc := range services {
		vars[svc.ID] = read[items[svc.ID]]
	}
	return k.store.DisconnectOnePassword(ctx, vars)
}
