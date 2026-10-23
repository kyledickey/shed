package deploy

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/kyledickey/shed/internal/store"
	"github.com/kyledickey/shed/internal/vars"
)

// Resolved holds the variables of a service with references expanded.
type Resolved struct {
	Env map[string]string
	// OnePassword holds the keys of Env whose value comes from the service's
	// 1Password Environment, sorted.
	OnePassword []string
}

// environments reads 1Password Environments, each at most once. A nil
// *environments treats every Environment as empty, for callers that must not
// call 1Password.
type environments struct {
	client OnePassword
	read   map[string]map[string]string // by Environment ID
	logf   func(format string, args ...any)
}

// newEnvironments returns an empty memo of Environment reads. logf, if not
// nil, reports each read.
func (d *Deployer) newEnvironments(logf func(format string, args ...any)) *environments {
	return &environments{client: d.onePassword, read: make(map[string]map[string]string), logf: logf}
}

// variables returns the variables of the Environment id, or none if id is
// empty.
func (e *environments) variables(ctx context.Context, id string) (map[string]string, error) {
	if e == nil || id == "" {
		return nil, nil
	}
	if v, ok := e.read[id]; ok {
		return v, nil
	}
	e.printf("==> Reading 1Password Environment %s", id)
	if e.client == nil {
		return nil, errors.New("1Password is not available")
	}
	v, err := e.client.Variables(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read 1Password Environment %s: %w", id, err)
	}
	e.printf("%d variables", len(v))
	e.read[id] = v
	return v, nil
}

func (e *environments) printf(format string, args ...any) {
	if e.logf != nil {
		e.logf(format, args...)
	}
}

// environment resolves the variables of svc. Each service's variables are
// layered, later winning: the ones shed injects, its 1Password Environment,
// and its own. References to other services of the project are expanded.
// sha is the commit being deployed. PORT comes from svc.Port.
//
// svc's Environment is read through envs up front; another service's only
// when a reference looks up a key that service does not define itself.
func (d *Deployer) environment(ctx context.Context, svc store.Service, project store.Project, sha string, envs *environments) (Resolved, error) {
	services, err := d.store.Services(ctx, svc.ProjectID)
	if err != nil {
		return Resolved{}, err
	}
	type layers struct {
		environment string
		injected    map[string]string
		own         map[string]string
		onePassword map[string]string
		read        bool
	}
	byName := make(map[string]*layers, len(services))
	for _, s := range services {
		own, err := d.store.Variables(ctx, s.ID)
		if err != nil {
			return Resolved{}, err
		}
		domains, err := d.store.Domains(ctx, s.ID)
		if err != nil {
			return Resolved{}, err
		}
		commit := ""
		if s.ID == svc.ID {
			commit = sha
			s.Port = svc.Port
		}
		byName[s.Name] = &layers{environment: s.OnePasswordEnvironment, injected: injected(project, s, domains, commit), own: own}
	}
	onePassword := func(l *layers) (map[string]string, error) {
		if !l.read {
			v, err := envs.variables(ctx, l.environment)
			if err != nil {
				return nil, err
			}
			l.onePassword, l.read = v, true
		}
		return l.onePassword, nil
	}

	self, ok := byName[svc.Name]
	if !ok {
		return Resolved{}, fmt.Errorf("deploy: service %s is not in its project", svc.Name)
	}
	selfOnePassword, err := onePassword(self)
	if err != nil {
		return Resolved{}, fmt.Errorf("deploy: %w", err)
	}
	keys := slices.Concat(slices.Collect(maps.Keys(self.injected)),
		slices.Collect(maps.Keys(selfOnePassword)), slices.Collect(maps.Keys(self.own)))
	slices.Sort(keys)
	keys = slices.Compact(keys)

	env, err := vars.Resolve(svc.Name, keys, func(service, key string) (string, bool, error) {
		l, ok := byName[service]
		if !ok {
			return "", false, nil
		}
		if v, ok := l.own[key]; ok {
			return v, true, nil
		}
		op, err := onePassword(l)
		if err != nil {
			return "", false, err
		}
		if v, ok := op[key]; ok {
			return v, true, nil
		}
		v, ok := l.injected[key]
		return v, ok, nil
	})
	if err != nil {
		return Resolved{}, fmt.Errorf("deploy: resolve variables: %w", err)
	}
	r := Resolved{Env: env}
	for key := range selfOnePassword {
		if _, own := self.own[key]; !own {
			r.OnePassword = append(r.OnePassword, key)
		}
	}
	slices.Sort(r.OnePassword)
	return r, nil
}

// ResolveVariables returns the variables of a service with references
// expanded, as its next deployment would see them, reading 1Password
// Environments. SHED_GIT_COMMIT_SHA is the commit of the active deployment,
// if any.
func (d *Deployer) ResolveVariables(ctx context.Context, serviceID string) (Resolved, error) {
	svc, err := d.store.Service(ctx, serviceID)
	if err != nil {
		return Resolved{}, err
	}
	project, err := d.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return Resolved{}, err
	}
	sha := ""
	switch dep, err := d.store.ActiveDeployment(ctx, serviceID); {
	case err == nil:
		sha = dep.CommitSHA
	case !errors.Is(err, store.ErrNotFound):
		return Resolved{}, err
	}
	return d.environment(ctx, svc, project, sha, d.newEnvironments(nil))
}
