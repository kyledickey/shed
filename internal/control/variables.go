package control

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Variables returns a service's own variables as saved, with references
// unexpanded.
func (p *Plane) Variables(ctx context.Context, serviceID string) (map[string]string, error) {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return nil, err
	}
	return p.store.Variables(ctx, serviceID)
}

// SetVariables replaces a service's own variables and returns them. They
// apply from its next deployment.
func (p *Plane) SetVariables(ctx context.Context, serviceID string, vars map[string]string) (map[string]string, error) {
	if _, err := p.store.Service(ctx, serviceID); err != nil {
		return nil, err
	}
	for k := range vars {
		if !envKey.MatchString(k) {
			return nil, errorf(ErrInvalid, "invalid variable name %q", k)
		}
	}
	if err := p.store.SetVariables(ctx, serviceID, vars); err != nil {
		return nil, err
	}
	return p.store.Variables(ctx, serviceID)
}

// ResolvedVariables returns the variables of a service's next deployment,
// injected ones included, with references expanded.
func (p *Plane) ResolvedVariables(ctx context.Context, serviceID string) (map[string]string, error) {
	return p.deployer.ResolveVariables(ctx, serviceID)
}

// VariableNames returns the names of the variables a service's next
// deployment would see: its own and the ones shed injects. It never returns
// values.
func (p *Plane) VariableNames(ctx context.Context, serviceID string) (VariableNames, error) {
	own, err := p.Variables(ctx, serviceID)
	if err != nil {
		return VariableNames{}, err
	}
	var out VariableNames
	for name, value := range own {
		out.Variables = append(out.Variables, VariableName{Name: name, Reference: strings.Contains(value, "${{")})
	}
	resolved, err := p.deployer.ResolveVariables(ctx, serviceID)
	if ctx.Err() != nil {
		return VariableNames{}, ctx.Err()
	}
	if err != nil {
		out.ResolveError = err.Error()
	}
	for name := range resolved {
		if _, ok := own[name]; !ok {
			out.Variables = append(out.Variables, VariableName{Name: name, Injected: true})
		}
	}
	slices.SortFunc(out.Variables, func(a, b VariableName) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}
