package control

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/kyledickey/shed/internal/store"
)

// CreateDomain routes a host name to a service. An empty host generates
// <service>-<project>.<base domain>. The domain is kept even if the proxy
// cannot be updated now; routesPending then reports that SyncRoutes is
// retrying.
func (p *Plane) CreateDomain(ctx context.Context, serviceID, host string) (d store.Domain, routesPending bool, err error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return store.Domain{}, false, err
	}
	host = strings.ToLower(strings.TrimSpace(host))
	generated := host == ""
	if generated {
		if host, err = p.generatedHost(ctx, svc); err != nil {
			return store.Domain{}, false, err
		}
	}
	if !validHost(host) {
		return store.Domain{}, false, errorf(ErrInvalid, "%q is not a valid host name", host)
	}
	if strings.EqualFold(host, p.dashboardHost) {
		return store.Domain{}, false, errorf(ErrConflict, "the dashboard hostname is reserved")
	}
	d, err = p.store.CreateDomain(ctx, svc.ID, host, generated)
	if errors.Is(err, store.ErrConflict) {
		return store.Domain{}, false, errorf(ErrConflict, "%s is already in use", host)
	}
	if err != nil {
		return store.Domain{}, false, err
	}
	return d, !p.applyDomainRoutes(ctx), nil
}

// DeleteDomain removes a domain. As with CreateDomain, routesPending reports
// that the proxy is not updated yet.
func (p *Plane) DeleteDomain(ctx context.Context, id string) (routesPending bool, err error) {
	if err := p.store.DeleteDomain(ctx, id); err != nil {
		return false, err
	}
	return !p.applyDomainRoutes(ctx), nil
}

// generatedHost returns <service>-<project>.<base domain> for svc.
func (p *Plane) generatedHost(ctx context.Context, svc store.Service) (string, error) {
	if p.baseDomain == "" {
		return "", errorf(ErrInvalid, "proxy.base_domain is not configured, so domains cannot be generated")
	}
	project, err := p.store.Project(ctx, svc.ProjectID)
	if err != nil {
		return "", err
	}
	label := svc.Name
	if ps := slug(project.Name); ps != "" {
		label += "-" + ps
	}
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	return label + "." + p.baseDomain, nil
}

// validHost reports whether host is a valid DNS host name.
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// CreateVolume adds a volume to a service, mounted at mountPath from its
// next deployment.
func (p *Plane) CreateVolume(ctx context.Context, serviceID, mountPath string) (store.Volume, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return store.Volume{}, err
	}
	if !path.IsAbs(mountPath) || path.Clean(mountPath) != mountPath || mountPath == "/" {
		return store.Volume{}, errorf(ErrInvalid, "mount path must be a clean absolute path other than /")
	}
	v, err := p.store.CreateVolume(ctx, svc.ID, mountPath)
	if errors.Is(err, store.ErrConflict) {
		return store.Volume{}, errorf(ErrConflict, "a volume is already mounted at %s", mountPath)
	}
	return v, err
}

// DeleteVolume deletes a volume and its data.
func (p *Plane) DeleteVolume(ctx context.Context, id string) error {
	return p.deployer.DeleteVolume(ctx, id)
}
