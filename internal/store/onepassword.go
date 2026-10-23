package store

import (
	"context"
	"fmt"
)

const onePasswordLinkCols = `service_id, environment, token, created_at`

func (s *Store) scanOnePasswordLink(r scanner) (OnePasswordLink, error) {
	var l OnePasswordLink
	if err := r.Scan(&l.ServiceID, &l.Environment, &l.Token, (*timestamp)(&l.CreatedAt)); err != nil {
		return OnePasswordLink{}, err
	}
	if l.Token == "" {
		return l, nil
	}
	token, err := s.crypt.open(l.Token, onePasswordLinkAD(l.ServiceID))
	if err != nil {
		return OnePasswordLink{}, fmt.Errorf("token: %w", err)
	}
	l.Token = token
	return l, nil
}

// OnePasswordLink returns the 1Password link of a service, or ErrNotFound if
// it has none.
func (s *Store) OnePasswordLink(ctx context.Context, serviceID string) (OnePasswordLink, error) {
	l, err := queryOne(ctx, s, s.scanOnePasswordLink,
		`SELECT `+onePasswordLinkCols+` FROM onepassword_links WHERE service_id = ?`, serviceID)
	if err != nil {
		return OnePasswordLink{}, fmt.Errorf("store: 1Password link of service %s: %w", serviceID, err)
	}
	return l, nil
}

// OnePasswordLinks returns the 1Password links of a project's services by
// service ID.
func (s *Store) OnePasswordLinks(ctx context.Context, projectID string) (map[string]OnePasswordLink, error) {
	links, err := queryAll(ctx, s, s.scanOnePasswordLink, `SELECT `+onePasswordLinkCols+`
		FROM onepassword_links
		WHERE service_id IN (SELECT id FROM services WHERE project_id = ?)`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: 1Password links of project %s: %w", projectID, err)
	}
	out := make(map[string]OnePasswordLink, len(links))
	for _, l := range links {
		out[l.ServiceID] = l
	}
	return out, nil
}

// SetOnePasswordLink links l.ServiceID to l.Environment with l.Token,
// replacing its current link, which keeps its creation time.
func (s *Store) SetOnePasswordLink(ctx context.Context, l OnePasswordLink) error {
	token := ""
	if l.Token != "" {
		token = s.crypt.seal(l.Token, onePasswordLinkAD(l.ServiceID))
	}
	err := s.exec(ctx, `INSERT INTO onepassword_links (`+onePasswordLinkCols+`) VALUES (?, ?, ?, ?)
		ON CONFLICT (service_id) DO UPDATE SET environment = excluded.environment, token = excluded.token`,
		l.ServiceID, l.Environment, token, formatTime(now()))
	if err != nil {
		return fmt.Errorf("store: set 1Password link of service %s: %w", l.ServiceID, err)
	}
	return nil
}

// DeleteOnePasswordLink removes the 1Password link of a service, if it has
// one.
func (s *Store) DeleteOnePasswordLink(ctx context.Context, serviceID string) error {
	if err := s.exec(ctx, `DELETE FROM onepassword_links WHERE service_id = ?`, serviceID); err != nil {
		return fmt.Errorf("store: delete 1Password link of service %s: %w", serviceID, err)
	}
	return nil
}
