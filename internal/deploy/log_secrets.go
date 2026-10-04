package deploy

import (
	"context"

	"github.com/kyledickey/shed/internal/store"
)

// logSecrets selects resolved, stored variables, excluding ordinary injected
// metadata such as ports and service names from literal redaction.
func (d *Deployer) logSecrets(ctx context.Context, svc store.Service, env map[string]string) ([]string, error) {
	own, err := d.store.Variables(ctx, svc.ID)
	if err != nil {
		return nil, err
	}
	secrets := make([]string, 0, len(own))
	for key := range own {
		if value := env[key]; value != "" {
			secrets = append(secrets, value)
		}
	}
	return secrets, nil
}
