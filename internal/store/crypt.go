package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the length in bytes of the key that encrypts secrets at rest.
const KeySize = 32

// sealedPrefix marks a value encrypted with AES-256-GCM. The rest of the value
// is the unpadded standard base64 of the nonce followed by the ciphertext.
const sealedPrefix = "v1:"

// crypter encrypts and decrypts column values. Every value is bound to its
// row through GCM additional data, so a value moved to another row fails to
// decrypt.
type crypter struct {
	aead cipher.AEAD
}

func newCrypter(key []byte) (*crypter, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("store: key is %d bytes, want %d", len(key), KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("store: key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("store: key: %w", err)
	}
	return &crypter{aead: aead}, nil
}

// seal encrypts plaintext bound to the additional data ad.
func (c *crypter) seal(plaintext, ad string) string {
	nonce := make([]byte, c.aead.NonceSize(), c.aead.NonceSize()+len(plaintext)+c.aead.Overhead())
	rand.Read(nonce)
	out := c.aead.Seal(nonce, nonce, []byte(plaintext), []byte(ad))
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(out)
}

// open decrypts a value produced by seal with the same additional data.
func (c *crypter) open(sealed, ad string) (string, error) {
	enc, ok := strings.CutPrefix(sealed, sealedPrefix)
	if !ok {
		return "", errors.New("store: value is not encrypted")
	}
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("store: decode encrypted value: %w", err)
	}
	n := c.aead.NonceSize()
	if len(raw) < n+c.aead.Overhead() {
		return "", errors.New("store: encrypted value is truncated")
	}
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], []byte(ad))
	if err != nil {
		return "", fmt.Errorf("store: decrypt value: %w", err)
	}
	return string(plain), nil
}

// settingAD returns the additional data binding a value to a settings row.
func settingAD(key string) string {
	return "settings\x00" + key
}

// variableAD returns the additional data binding a value to a variables row.
func variableAD(serviceID, key string) string {
	return "variables\x00" + serviceID + "\x00" + key
}

// destinationAD returns the additional data binding a secret access key to a
// backup_destinations row.
func destinationAD(id string) string {
	return "backup_destinations\x00" + id
}

// deploymentAD returns the additional data binding a runtime to a
// deployments row.
func deploymentAD(id string) string {
	return "deployments\x00" + id
}

// keyCheckSetting names the setting whose encrypted value proves that the
// database was encrypted with the key it is opened with.
const keyCheckSetting = "store.key_check"

// keyCheckValue is the plaintext of the key check setting.
const keyCheckValue = "shed"

// ensureEncrypted verifies that the database is encrypted with c's key. A
// database without a key check, created before values were encrypted, has
// every secret column encrypted and the key check added in one transaction.
func ensureEncrypted(ctx context.Context, db *sql.DB, c *crypter) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: encrypt: %w", err)
	}
	defer tx.Rollback()
	var check string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyCheckSetting).Scan(&check)
	switch {
	case err == nil:
		if v, err := c.open(check, settingAD(keyCheckSetting)); err != nil || v != keyCheckValue {
			return ErrWrongKey
		}
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("store: read key check: %w", err)
	}
	if err := encryptAll(ctx, tx, c); err != nil {
		return fmt.Errorf("store: encrypt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)`,
		keyCheckSetting, c.seal(keyCheckValue, settingAD(keyCheckSetting))); err != nil {
		return fmt.Errorf("store: write key check: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: encrypt: %w", err)
	}
	return nil
}

// encryptAll encrypts every plaintext value in the secret columns.
func encryptAll(ctx context.Context, tx *sql.Tx, c *crypter) error {
	tables := []struct {
		query  string // selects the row's key columns, then the value
		update string // sets the value, then binds the key columns
		ad     func(keys []string) string
	}{
		{
			`SELECT key, value FROM settings`,
			`UPDATE settings SET value = ? WHERE key = ?`,
			func(k []string) string { return settingAD(k[0]) },
		},
		{
			`SELECT service_id, key, value FROM variables`,
			`UPDATE variables SET value = ? WHERE service_id = ? AND key = ?`,
			func(k []string) string { return variableAD(k[0], k[1]) },
		},
		{
			`SELECT id, secret_access_key FROM backup_destinations`,
			`UPDATE backup_destinations SET secret_access_key = ? WHERE id = ?`,
			func(k []string) string { return destinationAD(k[0]) },
		},
		{
			`SELECT id, runtime FROM deployments WHERE runtime <> ''`,
			`UPDATE deployments SET runtime = ? WHERE id = ?`,
			func(k []string) string { return deploymentAD(k[0]) },
		},
	}
	for _, t := range tables {
		rows, err := readRows(ctx, tx, t.query)
		if err != nil {
			return err
		}
		for _, row := range rows {
			keys, value := row[:len(row)-1], row[len(row)-1]
			args := []any{c.seal(value, t.ad(keys))}
			for _, k := range keys {
				args = append(args, k)
			}
			if _, err := tx.ExecContext(ctx, t.update, args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// readRows reads every row of a query whose columns are all text.
func readRows(ctx context.Context, tx *sql.Tx, query string) ([][]string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for rows.Next() {
		row := make([]string, len(cols))
		dest := make([]any, len(cols))
		for i := range row {
			dest[i] = &row[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
