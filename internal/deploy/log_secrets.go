package deploy

import (
	"context"
	"slices"

	"github.com/kyledickey/shed/internal/store"
)

// secretKeys returns the keys of a service's own variables that resolve to a
// non-empty value in env. Their values are masked in logs; ordinary injected
// metadata such as ports and service names is not.
func (d *Deployer) secretKeys(ctx context.Context, svc store.Service, env map[string]string) ([]string, error) {
	own, err := d.store.Variables(ctx, svc.ID)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(own))
	for key := range own {
		if env[key] != "" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys, nil
}

// secretValues returns the values of env under keys.
func secretValues(env map[string]string, keys []string) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if v := env[key]; v != "" {
			values = append(values, v)
		}
	}
	return values
}
