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
	// Own holds the keys of the service's own variables, as opposed to the
	// ones shed injects, sorted.
	Own []string
}

// readVariables returns the own variables of a service.
type readVariables func(ctx context.Context, svc store.Service) (map[string]string, error)

// readOnce returns a reader that reads each service's variables with read
// at most once, for one deployment.
func readOnce(read readVariables) readVariables {
	done := make(map[string]map[string]string)
	return func(ctx context.Context, svc store.Service) (map[string]string, error) {
		if v, ok := done[svc.ID]; ok {
			return v, nil
		}
		v, err := read(ctx, svc)
		if err != nil {
			return nil, err
		}
		done[svc.ID] = v
		return v, nil
	}
}

// savedVariables reads the variables saved in shed.db.
type savedVariables struct{ store *store.Store }

func (s savedVariables) Get(ctx context.Context, svc store.Service) (map[string]string, error) {
	return s.store.Variables(ctx, svc.ID)
}

// saved reads the variables saved in shed.db, which never calls 1Password:
// while it is connected, shed.db has none.
func (d *Deployer) saved(ctx context.Context, svc store.Service) (map[string]string, error) {
	return savedVariables{d.store}.Get(ctx, svc)
}

// environment resolves the variables of svc. Each service's own variables
// override the ones shed injects into it. References to other services of
// the project are expanded. sha is the commit being deployed. PORT comes
// from svc.Port.
//
// svc's variables are read with read up front; another service's only when
// a reference needs one of them.
func (d *Deployer) environment(ctx context.Context, svc store.Service, project store.Project, sha string, read readVariables) (Resolved, error) {
	services, err := d.store.Services(ctx, svc.ProjectID)
	if err != nil {
		return Resolved{}, err
	}
	type layers struct {
		svc      store.Service
		injected map[string]string
		own      map[string]string
		read     bool
	}
	byName := make(map[string]*layers, len(services))
	for _, s := range services {
		domains, err := d.store.Domains(ctx, s.ID)
		if err != nil {
			return Resolved{}, err
		}
		commit := ""
		if s.ID == svc.ID {
			commit = sha
			s.Port = svc.Port
		}
		byName[s.Name] = &layers{svc: s, injected: injected(project, s, domains, commit)}
	}
	own := func(l *layers) (map[string]string, error) {
		if !l.read {
			v, err := read(ctx, l.svc)
			if err != nil {
				return nil, err
			}
			l.own, l.read = v, true
		}
		return l.own, nil
	}

	self, ok := byName[svc.Name]
	if !ok {
		return Resolved{}, fmt.Errorf("deploy: service %s is not in its project", svc.Name)
	}
	selfOwn, err := own(self)
	if err != nil {
		return Resolved{}, fmt.Errorf("deploy: %w", err)
	}
	ownKeys := slices.Sorted(maps.Keys(selfOwn))
	keys := slices.Concat(slices.Collect(maps.Keys(self.injected)), ownKeys)
	slices.Sort(keys)
	keys = slices.Compact(keys)

	env, err := vars.Resolve(svc.Name, keys, func(service, key string) (string, bool, error) {
		l, ok := byName[service]
		if !ok {
			return "", false, nil
		}
		o, err := own(l)
		if err != nil {
			return "", false, err
		}
		if v, ok := o[key]; ok {
			return v, true, nil
		}
		v, ok := l.injected[key]
		return v, ok, nil
	})
	if err != nil {
		return Resolved{}, fmt.Errorf("deploy: resolve variables: %w", err)
	}
	return Resolved{Env: env, Own: ownKeys}, nil
}

// ResolveVariables returns the variables of a service with references
// expanded, as its next deployment would see them. SHED_GIT_COMMIT_SHA is
// the commit of the active deployment, if any.
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
	return d.environment(ctx, svc, project, sha, d.variables.Get)
}
