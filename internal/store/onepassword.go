package store

import (
	"context"
	"errors"
	"fmt"
)

// The settings of the 1Password connection.
const (
	onePasswordToken     = "onepassword.token"
	onePasswordVault     = "onepassword.vault"
	onePasswordVaultName = "onepassword.vault_name"
)

// OnePasswordConnection is a 1Password connection: a service account token
// and the vault that holds the items.
type OnePasswordConnection struct {
	Token     string
	VaultID   string
	VaultName string
}

// OnePassword returns the 1Password connection, or ErrNotFound while
// 1Password is not connected. VaultName is empty for a connection saved
// before shed kept it.
func (s *Store) OnePassword(ctx context.Context) (OnePasswordConnection, error) {
	var c OnePasswordConnection
	var err error
	if c.Token, err = s.Setting(ctx, onePasswordToken); err != nil {
		return OnePasswordConnection{}, err
	}
	if c.VaultID, err = s.Setting(ctx, onePasswordVault); err != nil {
		return OnePasswordConnection{}, err
	}
	c.VaultName, err = s.Setting(ctx, onePasswordVaultName)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return OnePasswordConnection{}, err
	}
	return c, nil
}

// ConnectOnePassword deletes the variables of every service, whose values
// now live in 1Password, and saves the connection's vault and then its
// token, in one transaction.
func (s *Store) ConnectOnePassword(ctx context.Context, c OnePasswordConnection) error {
	if err := s.connectOnePassword(ctx, c); err != nil {
		return fmt.Errorf("store: connect 1Password: %w", err)
	}
	return nil
}

func (s *Store) connectOnePassword(ctx context.Context, c OnePasswordConnection) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM variables`); err != nil {
		return err
	}
	for _, kv := range [][2]string{
		{onePasswordVault, c.VaultID}, {onePasswordVaultName, c.VaultName}, {onePasswordToken, c.Token},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
			kv[0], s.crypt.seal(kv[1], settingAD(kv[0]))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DisconnectOnePassword saves the variables of services, by service ID, in
// place of their current ones, and deletes the 1Password connection and
// every service's item record, in one transaction.
func (s *Store) DisconnectOnePassword(ctx context.Context, vars map[string]map[string]string) error {
	if err := s.disconnectOnePassword(ctx, vars); err != nil {
		return fmt.Errorf("store: disconnect 1Password: %w", err)
	}
	return nil
}

func (s *Store) disconnectOnePassword(ctx context.Context, vars map[string]map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for serviceID, v := range vars {
		if _, err := tx.ExecContext(ctx, `DELETE FROM variables WHERE service_id = ?`, serviceID); err != nil {
			return err
		}
		for k, value := range v {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO variables (service_id, key, value) VALUES (?, ?, ?)`,
				serviceID, k, s.crypt.seal(value, variableAD(serviceID, k))); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{
		`DELETE FROM settings WHERE key IN ('` + onePasswordToken + `', '` + onePasswordVault + `', '` + onePasswordVaultName + `')`,
		`DELETE FROM onepassword_items`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OnePasswordItem returns the ID of the 1Password item that holds a
// service's variables, or ErrNotFound if none is recorded.
func (s *Store) OnePasswordItem(ctx context.Context, serviceID string) (string, error) {
	id, err := queryOne(ctx, s, scanString,
		`SELECT item_id FROM onepassword_items WHERE service_id = ?`, serviceID)
	if err != nil {
		return "", fmt.Errorf("store: 1Password item of service %s: %w", serviceID, err)
	}
	return id, nil
}

// OnePasswordItems returns the IDs of the 1Password items of services by
// service ID.
func (s *Store) OnePasswordItems(ctx context.Context) (map[string]string, error) {
	rows, err := queryAll(ctx, s, func(r scanner) ([2]string, error) {
		var row [2]string
		err := r.Scan(&row[0], &row[1])
		return row, err
	}, `SELECT service_id, item_id FROM onepassword_items`)
	if err != nil {
		return nil, fmt.Errorf("store: 1Password items: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r[0]] = r[1]
	}
	return out, nil
}

// SetOnePasswordItem records the 1Password item that holds a service's
// variables, in place of the one recorded.
func (s *Store) SetOnePasswordItem(ctx context.Context, serviceID, itemID string) error {
	err := s.exec(ctx, `INSERT INTO onepassword_items (service_id, item_id) VALUES (?, ?)
		ON CONFLICT (service_id) DO UPDATE SET item_id = excluded.item_id`, serviceID, itemID)
	if err != nil {
		return fmt.Errorf("store: set 1Password item of service %s: %w", serviceID, err)
	}
	return nil
}

func scanString(r scanner) (string, error) {
	var v string
	err := r.Scan(&v)
	return v, err
}
