package control

import (
	"context"
	"errors"
	"strings"

	"github.com/kyledickey/shed/internal/audit"
	"github.com/kyledickey/shed/internal/onepassword"
	"github.com/kyledickey/shed/internal/variables"
)

// OnePassword reports whether variables are kept in 1Password, without
// calling 1Password.
func (p *Plane) OnePassword(ctx context.Context) (variables.Status, error) {
	return p.variables.Status(ctx)
}

// OnePasswordVaults returns the vaults a service account token can access,
// sorted by name. The request is invalid if 1Password rejects the token.
func (p *Plane) OnePasswordVaults(ctx context.Context, token string) ([]onepassword.Vault, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errorf(ErrInvalid, "a service account token is required")
	}
	vaults, err := p.variables.Vaults(ctx, token)
	if err != nil {
		return nil, onePasswordError(ctx, err)
	}
	return vaults, nil
}

// ConnectOnePassword moves every service's variables into a 1Password vault
// and keeps them there from then on. vault is the ID of a vault the token
// can access; if it is empty, the token's only vault is used. The request is
// invalid if 1Password rejects the token or there is no such vault. If
// 1Password fails partway, shed.db keeps the variables and connecting again
// picks up where it stopped.
func (p *Plane) ConnectOnePassword(ctx context.Context, token, vault string) (variables.Status, error) {
	err := p.connectOnePassword(ctx, token, vault)
	status, serr := p.variables.Status(ctx)
	if err == nil {
		err = serr
	}
	p.record(ctx, err, audit.Event{Action: "onepassword.connect", Detail: map[string]any{"vault": status.Vault}})
	if err != nil {
		return variables.Status{}, err
	}
	return status, nil
}

func (p *Plane) connectOnePassword(ctx context.Context, token, vault string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errorf(ErrInvalid, "a service account token is required")
	}
	if err := p.variables.Connect(ctx, token, strings.TrimSpace(vault)); err != nil {
		return onePasswordError(ctx, err)
	}
	return nil
}

// onePasswordError makes a rejected token or a missing vault an invalid
// request.
func onePasswordError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return err
	case errors.Is(err, onepassword.ErrInvalidToken):
		return errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), "onepassword: "))
	case errors.Is(err, variables.ErrVault):
		return errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), variables.ErrVault.Error()+": "))
	}
	return err
}

// DisconnectOnePassword copies every service's variables from 1Password back
// into shed.db and forgets the token. The vault and its items stay in
// 1Password. If 1Password fails, nothing changes.
func (p *Plane) DisconnectOnePassword(ctx context.Context) (variables.Status, error) {
	was, _ := p.variables.Status(ctx)
	err := p.variables.Disconnect(ctx)
	p.record(ctx, err, audit.Event{Action: "onepassword.disconnect", Detail: map[string]any{"vault": was.Vault}})
	if err != nil {
		return variables.Status{}, err
	}
	return p.variables.Status(ctx)
}
