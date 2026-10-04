package store

import (
	"context"
	"fmt"
	"strings"
)

const volumeCols = `id, service_id, mount_path, created_at`

func scanVolume(r scanner) (Volume, error) {
	var v Volume
	err := r.Scan(&v.ID, &v.ServiceID, &v.MountPath, (*timestamp)(&v.CreatedAt))
	return v, err
}

// CreateVolume adds a volume mounted at mountPath to a service. It returns
// ErrConflict if the path is already mounted.
func (s *Store) CreateVolume(ctx context.Context, serviceID, mountPath string) (Volume, error) {
	v := Volume{ID: NewID(), ServiceID: serviceID, MountPath: mountPath, CreatedAt: now()}
	err := s.exec(ctx, `INSERT INTO volumes (`+volumeCols+`) VALUES (?, ?, ?, ?)`,
		v.ID, v.ServiceID, v.MountPath, formatTime(v.CreatedAt))
	if err != nil {
		return Volume{}, fmt.Errorf("store: create volume %q: %w", mountPath, err)
	}
	return v, nil
}

// Volumes returns a service's volumes, oldest first.
func (s *Store) Volumes(ctx context.Context, serviceID string) ([]Volume, error) {
	vs, err := queryAll(ctx, s, scanVolume, `SELECT `+volumeCols+`
		FROM volumes WHERE service_id = ? ORDER BY created_at, rowid`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("store: volumes of service %s: %w", serviceID, err)
	}
	return vs, nil
}

// Volume returns the volume with the given ID, or ErrNotFound.
func (s *Store) Volume(ctx context.Context, id string) (Volume, error) {
	v, err := queryOne(ctx, s, scanVolume, `SELECT `+volumeCols+` FROM volumes WHERE id = ?`, id)
	if err != nil {
		return Volume{}, fmt.Errorf("store: volume %s: %w", id, err)
	}
	return v, nil
}

// DeleteVolume deletes a volume record. It returns ErrNotFound for an unknown
// volume.
func (s *Store) DeleteVolume(ctx context.Context, id string) error {
	if err := s.execOne(ctx, `DELETE FROM volumes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete volume %s: %w", id, err)
	}
	return nil
}

const domainCols = `id, service_id, host, generated, created_at`

func scanDomain(r scanner) (Domain, error) {
	var d Domain
	err := r.Scan(&d.ID, &d.ServiceID, &d.Host, &d.Generated, (*timestamp)(&d.CreatedAt))
	return d, err
}

// CreateDomain routes host to a service. Hosts are stored lowercase. It
// returns ErrConflict if the host is already in use.
func (s *Store) CreateDomain(ctx context.Context, serviceID, host string, generated bool) (Domain, error) {
	d := Domain{
		ID: NewID(), ServiceID: serviceID, Host: strings.ToLower(host),
		Generated: generated, CreatedAt: now(),
	}
	err := s.exec(ctx, `INSERT INTO domains (`+domainCols+`) VALUES (?, ?, ?, ?, ?)`,
		d.ID, d.ServiceID, d.Host, d.Generated, formatTime(d.CreatedAt))
	if err != nil {
		return Domain{}, fmt.Errorf("store: create domain %q: %w", d.Host, err)
	}
	return d, nil
}

// Domains returns a service's domains, oldest first.
func (s *Store) Domains(ctx context.Context, serviceID string) ([]Domain, error) {
	ds, err := queryAll(ctx, s, scanDomain, `SELECT `+domainCols+`
		FROM domains WHERE service_id = ? ORDER BY created_at, rowid`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("store: domains of service %s: %w", serviceID, err)
	}
	return ds, nil
}

// AllDomains returns every domain, oldest first.
func (s *Store) AllDomains(ctx context.Context) ([]Domain, error) {
	ds, err := queryAll(ctx, s, scanDomain, `SELECT `+domainCols+` FROM domains ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list domains: %w", err)
	}
	return ds, nil
}

// Domain returns the domain with the given ID, or ErrNotFound.
func (s *Store) Domain(ctx context.Context, id string) (Domain, error) {
	d, err := queryOne(ctx, s, scanDomain, `SELECT `+domainCols+` FROM domains WHERE id = ?`, id)
	if err != nil {
		return Domain{}, fmt.Errorf("store: domain %s: %w", id, err)
	}
	return d, nil
}

// DeleteDomain deletes a domain. It returns ErrNotFound for an unknown
// domain.
func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	if err := s.execOne(ctx, `DELETE FROM domains WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete domain %s: %w", id, err)
	}
	return nil
}
