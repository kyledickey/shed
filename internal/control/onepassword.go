package control

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/kyledickey/shed/internal/onepassword"
)

// environmentID matches a 1Password Environment ID.
var environmentID = regexp.MustCompile(`^[a-z0-9]{1,64}$`)

// OnePasswordStatus says whether shed can read 1Password Environments.
type OnePasswordStatus struct {
	// Connected is set while a service account token is saved.
	Connected bool
}

// OnePassword returns whether a 1Password service account token is saved.
func (p *Plane) OnePassword(ctx context.Context) (OnePasswordStatus, error) {
	ok, err := p.onePassword.Connected(ctx)
	if err != nil {
		return OnePasswordStatus{}, err
	}
	return OnePasswordStatus{Connected: ok}, nil
}

// ConnectOnePassword signs in to 1Password with a service account token and
// saves it in place of the current one. A token 1Password rejects is invalid
// and not saved.
func (p *Plane) ConnectOnePassword(ctx context.Context, token string) (OnePasswordStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return OnePasswordStatus{}, errorf(ErrInvalid, "a service account token is required")
	}
	err := p.onePassword.Connect(ctx, token)
	if errors.Is(err, onepassword.ErrInvalidToken) {
		return OnePasswordStatus{}, errorf(ErrInvalid, "%s", strings.TrimPrefix(err.Error(), "onepassword: "))
	}
	if err != nil {
		return OnePasswordStatus{}, err
	}
	return OnePasswordStatus{Connected: true}, nil
}

// DisconnectOnePassword forgets the saved 1Password token. Services keep
// their linked Environments, which fail their next deployment until a token
// is connected again.
func (p *Plane) DisconnectOnePassword(ctx context.Context) error {
	return p.onePassword.Disconnect(ctx)
}

// OnePasswordVariables returns the variables of the 1Password Environment a
// service links, or none if it links none.
func (p *Plane) OnePasswordVariables(ctx context.Context, serviceID string) (map[string]string, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	if svc.OnePasswordEnvironment == "" {
		return map[string]string{}, nil
	}
	vars, err := p.onePassword.Variables(ctx, svc.OnePasswordEnvironment)
	switch {
	case err == nil:
		return vars, nil
	case errors.Is(err, onepassword.ErrNotConnected), ctx.Err() != nil:
		return nil, err
	default:
		return nil, errorf(ErrUpstream, "%s", strings.TrimPrefix(err.Error(), "onepassword: "))
	}
}
