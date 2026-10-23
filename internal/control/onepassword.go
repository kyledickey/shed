package control

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/kyledickey/shed/internal/store"
)

// environmentID matches a 1Password Environment ID.
var environmentID = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// OnePasswordLink is the 1Password Environment a service reads variables
// from. It never holds the token.
type OnePasswordLink struct {
	Environment string
	// HasToken is set while a service account token is saved for the link.
	HasToken bool
}

// LinkOnePassword links a service to a 1Password Environment, read with a
// service account token, in place of its current link. A blank token keeps
// the token of the current link. The Environment is read once with the token
// first; if that fails, the request is invalid and nothing is saved.
func (p *Plane) LinkOnePassword(ctx context.Context, serviceID, environment, token string) (ServiceView, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return ServiceView{}, err
	}
	environment, token = strings.TrimSpace(environment), strings.TrimSpace(token)
	if !environmentID.MatchString(environment) {
		return ServiceView{}, errorf(ErrInvalid, "1Password Environment ID must be up to 64 lowercase letters and digits")
	}
	if token == "" {
		switch link, err := p.store.OnePasswordLink(ctx, serviceID); {
		case err == nil:
			token = link.Token
		case !errors.Is(err, store.ErrNotFound):
			return ServiceView{}, err
		}
	}
	if token == "" {
		return ServiceView{}, errorf(ErrInvalid, "a service account token is required")
	}
	if _, err := p.onePassword.Variables(ctx, token, environment); err != nil {
		if ctx.Err() != nil {
			return ServiceView{}, ctx.Err()
		}
		return ServiceView{}, errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), "onepassword: "))
	}
	if err := p.store.SetOnePasswordLink(ctx, store.OnePasswordLink{ServiceID: serviceID, Environment: environment, Token: token}); err != nil {
		return ServiceView{}, err
	}
	return p.withStatus(ctx, svc)
}

// UnlinkOnePassword removes a service's 1Password link and its token, if it
// has one.
func (p *Plane) UnlinkOnePassword(ctx context.Context, serviceID string) (ServiceView, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return ServiceView{}, err
	}
	if err := p.store.DeleteOnePasswordLink(ctx, serviceID); err != nil {
		return ServiceView{}, err
	}
	return p.withStatus(ctx, svc)
}

// OnePasswordVariables returns the variables of the 1Password Environment a
// service links, or none if it links none.
func (p *Plane) OnePasswordVariables(ctx context.Context, serviceID string) (map[string]string, error) {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return nil, err
	}
	link, err := p.store.OnePasswordLink(ctx, serviceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return map[string]string{}, nil
	case err != nil:
		return nil, err
	case link.Token == "":
		return nil, errorf(ErrConflict, "this service's 1Password link has no service account token; add one on its Variables tab")
	}
	return p.onePassword.Variables(ctx, link.Token, link.Environment)
}

// onePasswordLink returns the 1Password link of a service, or nil if it has
// none.
func (p *Plane) onePasswordLink(ctx context.Context, serviceID string) (*OnePasswordLink, error) {
	link, err := p.store.OnePasswordLink(ctx, serviceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	return &OnePasswordLink{Environment: link.Environment, HasToken: link.Token != ""}, nil
}
