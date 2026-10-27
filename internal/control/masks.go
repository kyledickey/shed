package control

import (
	"context"
	"io"
	"maps"
	"slices"

	"github.com/kyledickey/shed/internal/build"
	"github.com/kyledickey/shed/internal/store"
)

// A Mask replaces secret values in a string. It is safe for concurrent use.
type Mask func(string) string

// newMask returns a Mask of values, with the limits of build secret handling.
func newMask(values []string) Mask {
	return build.NewRedactor(io.Discard, values).Redact
}

// ShedMask masks what shed itself writes, as ShedLog does: the saved values
// of every service's variables.
func (p *Plane) ShedMask(ctx context.Context) (Mask, error) {
	services, err := p.store.AllServices(ctx)
	if err != nil {
		return nil, err
	}
	values, err := p.savedValues(ctx, services)
	if err != nil {
		return nil, err
	}
	return newMask(values), nil
}

// RuntimeMask masks the output of a deployment's container, as RuntimeLogs
// does: the values of the variables it was started with.
func (p *Plane) RuntimeMask(ctx context.Context, deploymentID string) (Mask, error) {
	values, err := p.runtimeValues(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	return newMask(values), nil
}

// TelemetryMask masks what a project's service sends to the telemetry
// gateway: the saved values of every variable in the project and, given a
// deployment, the values its container was started with.
func (p *Plane) TelemetryMask(ctx context.Context, projectID, deploymentID string) (Mask, error) {
	services, err := p.store.Services(ctx, projectID)
	if err != nil {
		return nil, err
	}
	values, err := p.savedValues(ctx, services)
	if err != nil {
		return nil, err
	}
	if deploymentID != "" {
		runtime, err := p.runtimeValues(ctx, deploymentID)
		if err != nil {
			return nil, err
		}
		values = append(values, runtime...)
	}
	return newMask(values), nil
}

// savedValues returns the values of the variables of services saved in
// shed.db.
func (p *Plane) savedValues(ctx context.Context, services []store.Service) ([]string, error) {
	var values []string
	for _, svc := range services {
		vars, err := p.store.Variables(ctx, svc.ID)
		if err != nil {
			return nil, err
		}
		values = slices.AppendSeq(values, maps.Values(vars))
	}
	return values, nil
}

func (p *Plane) runtimeValues(ctx context.Context, deploymentID string) ([]string, error) {
	dep, err := p.store.Deployment(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	return p.deployer.RuntimeSecrets(ctx, dep)
}
