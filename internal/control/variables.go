package control

import (
	"cmp"
	"context"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Variables returns a service's own variables as saved, with references
// unexpanded. While 1Password is connected they are read from the service's
// item.
func (p *Plane) Variables(ctx context.Context, serviceID string) (map[string]string, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		p.recordService(ctx, err, "service.variables.read", serviceID, nil)
		return nil, err
	}
	vars, err := p.variables.Get(ctx, svc)
	p.record(ctx, err, serviceEvent("service.variables.read", svc, nil))
	return vars, err
}

// SetVariables replaces a service's own variables and returns them. They
// apply from its next deployment.
func (p *Plane) SetVariables(ctx context.Context, serviceID string, vars map[string]string) (map[string]string, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		p.recordService(ctx, err, "service.variables.update", serviceID, nil)
		return nil, err
	}
	if vars == nil {
		vars = map[string]string{}
	}
	// Old values are compared, never recorded. If they cannot be read, every
	// key is recorded as given.
	detail := map[string]any{"keys": slices.Sorted(maps.Keys(vars))}
	for k := range vars {
		if !envKey.MatchString(k) {
			err := errorf(ErrInvalid, "invalid variable name %q", k)
			p.record(ctx, err, serviceEvent("service.variables.update", svc, detail))
			return nil, err
		}
	}
	if old, err := p.variables.Get(ctx, svc); err == nil {
		detail = changedKeys(old, vars)
	}
	err = p.variables.Set(ctx, svc, vars)
	p.record(ctx, err, serviceEvent("service.variables.update", svc, detail))
	if err != nil {
		return nil, err
	}
	return vars, nil
}

// changedKeys returns the names of the variables added, changed, and
// removed from old to vars. Values are never part of it.
func changedKeys(old, vars map[string]string) map[string]any {
	added, changed, removed := []string{}, []string{}, []string{}
	for k, v := range vars {
		switch was, ok := old[k]; {
		case !ok:
			added = append(added, k)
		case was != v:
			changed = append(changed, k)
		}
	}
	for k := range old {
		if _, ok := vars[k]; !ok {
			removed = append(removed, k)
		}
	}
	slices.Sort(added)
	slices.Sort(changed)
	slices.Sort(removed)
	return map[string]any{"added": added, "changed": changed, "removed": removed}
}

// ResolvedVariables returns the variables of a service's next deployment,
// injected ones included, with references expanded.
func (p *Plane) ResolvedVariables(ctx context.Context, serviceID string) (map[string]string, error) {
	r, err := p.deployer.ResolveVariables(ctx, serviceID)
	p.recordService(ctx, err, "service.variables.resolved.read", serviceID, nil)
	if err != nil {
		return nil, err
	}
	return r.Env, nil
}

// VariableNames returns the names of the variables a service's next
// deployment would see: its own and the ones shed injects. It never returns
// values.
func (p *Plane) VariableNames(ctx context.Context, serviceID string) (VariableNames, error) {
	resolved, err := p.deployer.ResolveVariables(ctx, serviceID)
	if ctx.Err() != nil {
		return VariableNames{}, ctx.Err()
	}
	if err != nil && resolved.Saved == nil {
		return VariableNames{}, err
	}
	var out VariableNames
	if err != nil {
		out.ResolveError = err.Error()
	}
	for name, value := range resolved.Saved {
		out.Variables = append(out.Variables, VariableName{Name: name, Reference: strings.Contains(value, "${{")})
	}
	for name := range resolved.Env {
		if _, ok := resolved.Saved[name]; ok {
			continue
		}
		out.Variables = append(out.Variables, VariableName{Name: name, Injected: true})
	}
	slices.SortFunc(out.Variables, func(a, b VariableName) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}
