package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnePasswordItems(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	web := mustService(t, s, Service{ProjectID: p.ID, Name: "web", Kind: "app"})
	db := mustService(t, s, Service{ProjectID: p.ID, Name: "db", Kind: "postgres"})

	if _, err := s.OnePasswordItem(ctx, web.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OnePasswordItem() of a service without one = %v, want ErrNotFound", err)
	}
	for _, kv := range [][2]string{{web.ID, "i1"}, {db.ID, "i2"}, {web.ID, "i3"}} {
		if err := s.SetOnePasswordItem(ctx, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.OnePasswordItem(ctx, web.ID); err != nil || got != "i3" {
		t.Errorf("OnePasswordItem() = %q, %v; want the item recorded last", got, err)
	}
	if err := s.DeleteService(ctx, db.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.OnePasswordItems(ctx); err != nil || !maps.Equal(got, map[string]string{web.ID: "i3"}) {
		t.Errorf("OnePasswordItems() = %v, %v; want web's only", got, err)
	}
}

func TestConnectAndDisconnectOnePassword(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	web := mustService(t, s, Service{ProjectID: p.ID, Name: "web", Kind: "app"})
	db := mustService(t, s, Service{ProjectID: p.ID, Name: "db", Kind: "postgres"})
	if err := s.SetVariables(ctx, web.ID, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OnePassword(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OnePassword() before connecting = %v, want ErrNotFound", err)
	}

	if err := s.SetOnePasswordItem(ctx, web.ID, "i1"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConnectOnePassword(ctx, "ops_token", "vault1"); err != nil {
		t.Fatal(err)
	}
	if token, vault, err := s.OnePassword(ctx); err != nil || token != "ops_token" || vault != "vault1" {
		t.Errorf("OnePassword() = %q, %q, %v", token, vault, err)
	}
	if rows := rawRows(t, s, `SELECT key FROM variables`); len(rows) != 0 {
		t.Errorf("variables after connecting = %v, want none", rows)
	}
	for _, r := range rawRows(t, s, `SELECT value FROM settings WHERE key LIKE 'onepassword.%'`) {
		if !strings.HasPrefix(r[0], sealedPrefix) {
			t.Errorf("1Password setting stored as %q, want encrypted", r[0])
		}
	}

	if err := s.DisconnectOnePassword(ctx, map[string]map[string]string{
		web.ID: {"A": "2", "B": "3"},
		db.ID:  nil,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.OnePassword(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("OnePassword() after disconnecting = %v, want ErrNotFound", err)
	}
	if items, _ := s.OnePasswordItems(ctx); len(items) != 0 {
		t.Errorf("items after disconnecting = %v", items)
	}
	if got, err := s.Variables(ctx, web.ID); err != nil || !maps.Equal(got, map[string]string{"A": "2", "B": "3"}) {
		t.Errorf("Variables() after disconnecting = %v, %v", got, err)
	}
}

func TestMigrateOnePasswordItems(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shed.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	migrateTo(t, db, 15)
	for _, q := range []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('p', 'p', '2026-10-01T00:00:00.000Z')`,
		`INSERT INTO services (id, project_id, name, kind, created_at) VALUES ('web', 'p', 'web', 'app', '2026-10-01T00:00:00.000Z')`,
		`INSERT INTO onepassword_links (service_id, environment, token, created_at) VALUES ('web', 'env1', '', '2026-10-01T00:00:00.000Z')`,
		`INSERT INTO settings (key, value) VALUES ('onepassword.token', 't'), ('onepassword.vault', 'v'), ('other', 'kept')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if rows := rawRows(t, s, `SELECT name FROM sqlite_master WHERE name = 'onepassword_links'`); len(rows) != 0 {
		t.Error("onepassword_links still exists")
	}
	if err := s.SetOnePasswordItem(ctx, "web", "i1"); err != nil {
		t.Errorf("SetOnePasswordItem() after migrating: %v", err)
	}
	if _, _, err := s.OnePassword(ctx); !errors.Is(err, ErrNotFound) {
		t.Errorf("1Password settings survived the migration: %v", err)
	}
	if v, err := s.Setting(ctx, "other"); err != nil || v != "kept" {
		t.Errorf("other setting = %q, %v; want kept", v, err)
	}
}
