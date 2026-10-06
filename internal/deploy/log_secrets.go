package deploy

import (
	"context"
	"maps"
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

// maskedValues returns the values to mask in a deployment's build log when
// it is shown outside the dashboard: the secrets its container was started
// with, the saved values of the variables of every service in its project,
// and the service's own variables as they resolve now. Resolved values are
// left out if the variables do not resolve; the saved values they are made
// of are masked anyway.
func (d *Deployer) maskedValues(ctx context.Context, dep store.Deployment) ([]string, error) {
	var values []string
	if dep.Runtime != nil {
		values = secretValues(dep.Runtime.Env, dep.Runtime.SecretKeys)
	}
	svc, err := d.store.Service(ctx, dep.ServiceID)
	if err != nil {
		return nil, err
	}
	services, err := d.store.Services(ctx, svc.ProjectID)
	if err != nil {
		return nil, err
	}
	var own map[string]string
	for _, s := range services {
		vars, err := d.store.Variables(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		if s.ID == svc.ID {
			own = vars
		}
		values = slices.AppendSeq(values, maps.Values(vars))
	}
	project, err := d.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return nil, err
	}
	if env, err := d.environment(ctx, svc, project, dep.CommitSHA); err == nil {
		values = append(values, secretValues(env, slices.Sorted(maps.Keys(own)))...)
	}
	return values, nil
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
