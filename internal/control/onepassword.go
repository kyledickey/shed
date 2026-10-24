package control

import (
	"context"
	"errors"
	"strings"

	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/variables"
)

// OnePassword reports whether variables are kept in 1Password, without
// calling 1Password.
func (p *Plane) OnePassword(ctx context.Context) (variables.Status, error) {
	return p.variables.Status(ctx)
}

// ConnectOnePassword moves every service's variables into a 1Password vault
// that a service account token can create, and keeps them there from then
// on. The request is invalid if 1Password rejects the token. If 1Password
// fails partway, shed.db keeps the variables and connecting again picks up
// where it stopped.
func (p *Plane) ConnectOnePassword(ctx context.Context, token string) (variables.Status, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return variables.Status{}, errorf(ErrInvalid, "a service account token is required")
	}
	if err := p.variables.Connect(ctx, token); err != nil {
		if errors.Is(err, onepassword.ErrInvalidToken) && ctx.Err() == nil {
			return variables.Status{}, errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), "onepassword: "))
		}
		return variables.Status{}, err
	}
	return p.variables.Status(ctx)
}

// DisconnectOnePassword copies every service's variables from 1Password back
// into shed.db and forgets the token. The vault and its items stay in
// 1Password. If 1Password fails, nothing changes.
func (p *Plane) DisconnectOnePassword(ctx context.Context) (variables.Status, error) {
	if err := p.variables.Disconnect(ctx); err != nil {
		return variables.Status{}, err
	}
	return p.variables.Status(ctx)
}
