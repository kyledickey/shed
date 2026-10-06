package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

var testKey = bytes.Repeat([]byte{0x5e}, KeySize)

func TestCrypter(t *testing.T) {
	c, err := newCrypter(testKey)
	if err != nil {
		t.Fatal(err)
	}
	other, err := newCrypter(bytes.Repeat([]byte{0x7a}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed := c.seal("secret", settingAD("k"))
	if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, "secret") {
		t.Fatalf("seal() = %q", sealed)
	}
	if again := c.seal("secret", settingAD("k")); again == sealed {
		t.Error("seal() repeated its nonce")
	}

	tests := []struct {
		name    string
		c       *crypter
		sealed  string
		ad      string
		want    string
		wantErr bool
	}{
		{"round trip", c, sealed, settingAD("k"), "secret", false},
		{"empty", c, c.seal("", variableAD("s", "K")), variableAD("s", "K"), "", false},
		{"other row", c, sealed, settingAD("j"), "", true},
		{"other table", c, sealed, destinationAD("k"), "", true},
		{"other key", other, sealed, settingAD("k"), "", true},
		{"plaintext", c, "secret", settingAD("k"), "", true},
		{"bad base64", c, "v1:!!", settingAD("k"), "", true},
		{"truncated", c, sealed[:10], settingAD("k"), "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.open(tt.sealed, tt.ad)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("open() = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestOpenRejectsBadKey(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33} {
		if s, err := Open(filepath.Join(t.TempDir(), "shed.db"), make([]byte, n)); err == nil {
			s.Close()
			t.Errorf("Open() with a %d-byte key succeeded", n)
		}
	}
}

func TestOpenEncryptsPlaintext(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shed.db")
	s, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "app"})
	d, err := s.PutBackupDestination(ctx, BackupDestination{Endpoint: "https://s3", Bucket: "b", SecretAccessKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Rewind the database to before encryption: plaintext values and no
	// key check.
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`DELETE FROM settings WHERE key = ?`, []any{keyCheckSetting}},
		{`INSERT INTO settings (key, value) VALUES ('github.app', 'pem')`, nil},
		{`INSERT INTO variables (service_id, key, value) VALUES (?, 'TOKEN', 'abc'), (?, 'EMPTY', '')`, []any{sv.ID, sv.ID}},
		{`UPDATE backup_destinations SET secret_access_key = 'SK' WHERE id = ?`, []any{d.ID}},
	} {
		if _, err := s.db.ExecContext(ctx, q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`SELECT value FROM settings`,
		`SELECT value FROM variables`,
		`SELECT secret_access_key FROM backup_destinations`,
	} {
		rows := rawRows(t, s, q)
		if len(rows) == 0 {
			t.Errorf("%s: no rows", q)
		}
		for _, r := range rows {
			if !strings.HasPrefix(r[0], sealedPrefix) {
				t.Errorf("%s: stored %q, want encrypted", q, r[0])
			}
		}
	}
	if got, err := s.Setting(ctx, "github.app"); err != nil || got != "pem" {
		t.Errorf("Setting() = %q, %v; want pem", got, err)
	}
	if got, err := s.Variables(ctx, sv.ID); err != nil || len(got) != 2 || got["TOKEN"] != "abc" || got["EMPTY"] != "" {
		t.Errorf("Variables() = %v, %v", got, err)
	}
	if got, err := s.BackupDestination(ctx, d.ID); err != nil || got.SecretAccessKey != "SK" {
		t.Errorf("BackupDestination() = %+v, %v; want secret SK", got, err)
	}
	s.Close()

	// Encryption happens once: reopening keeps the values.
	s, err = Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, err := s.Setting(ctx, "github.app"); err != nil || got != "pem" {
		t.Errorf("Setting() after reopen = %q, %v; want pem", got, err)
	}
}

// rawRows returns the stored values a query selects, bypassing decryption.
func rawRows(t *testing.T, s *Store, query string) [][]string {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	rows, err := readRows(context.Background(), tx, query)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestOpenWrongKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shed.db")
	s, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err := Open(path, bytes.Repeat([]byte{1}, KeySize)); !errors.Is(err, ErrWrongKey) {
		if err == nil {
			s.Close()
		}
		t.Errorf("Open() with another key error = %v, want ErrWrongKey", err)
	}
}

func TestStoredValuesBoundToRow(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if err := s.SetSetting(ctx, "a", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) SELECT 'b', value FROM settings WHERE key = 'a'`); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Setting(ctx, "b"); err == nil {
		t.Errorf("Setting() of a value copied from another row = %q, want error", got)
	}
}
