package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestOnePasswordLinks(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	p := mustProject(t, s, "p")
	web := mustService(t, s, Service{ProjectID: p.ID, Name: "web", Kind: "app"})
	db := mustService(t, s, Service{ProjectID: p.ID, Name: "db", Kind: "postgres"})
	other := mustService(t, s, Service{ProjectID: mustProject(t, s, "q").ID, Name: "web", Kind: "app"})

	if _, err := s.OnePasswordLink(ctx, web.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("OnePasswordLink() of an unlinked service error = %v, want ErrNotFound", err)
	}
	for _, l := range []OnePasswordLink{
		{ServiceID: web.ID, Environment: "env1", Token: "ops_web"},
		{ServiceID: db.ID, Environment: "env2"},
		{ServiceID: other.ID, Environment: "env3", Token: "ops_other"},
	} {
		if err := s.SetOnePasswordLink(ctx, l); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.OnePasswordLink(ctx, web.ID)
	if err != nil || got.Environment != "env1" || got.Token != "ops_web" || got.CreatedAt.IsZero() {
		t.Fatalf("OnePasswordLink() = %+v, %v; want env1 with its token", got, err)
	}
	created := got.CreatedAt

	if err := s.SetOnePasswordLink(ctx, OnePasswordLink{ServiceID: web.ID, Environment: "env4", Token: "ops_new"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.OnePasswordLink(ctx, web.ID); got.Environment != "env4" || got.Token != "ops_new" || !got.CreatedAt.Equal(created) {
		t.Errorf("after relinking: %+v, want env4, ops_new, created %v", got, created)
	}

	links, err := s.OnePasswordLinks(ctx, p.ID)
	if err != nil || len(links) != 2 || links[web.ID].Token != "ops_new" || links[db.ID].Environment != "env2" || links[db.ID].Token != "" {
		t.Errorf("OnePasswordLinks() = %+v, %v; want web and db only", links, err)
	}

	for _, r := range rawRows(t, s, `SELECT token FROM onepassword_links WHERE token <> ''`) {
		if r[0] == "ops_new" || r[0] == "ops_other" {
			t.Errorf("token stored in plaintext: %q", r[0])
		}
	}

	if err := s.DeleteOnePasswordLink(ctx, web.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteOnePasswordLink(ctx, web.ID); err != nil {
		t.Errorf("deleting a missing link: %v", err)
	}
	if _, err := s.OnePasswordLink(ctx, web.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("OnePasswordLink() after delete error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteService(ctx, db.ID); err != nil {
		t.Fatal(err)
	}
	if links, _ := s.OnePasswordLinks(ctx, p.ID); len(links) != 0 {
		t.Errorf("links after deleting the service = %+v, want none", links)
	}
}

func TestMigrateOnePasswordLinks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "shed.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	migrateTo(t, db, 14)
	for _, q := range []string{
		`INSERT INTO projects (id, name, created_at) VALUES ('p', 'p', '2026-10-01T00:00:00.000Z')`,
		`INSERT INTO services (id, project_id, name, kind, onepassword_environment, created_at)
			VALUES ('linked', 'p', 'web', 'app', 'env1', '2026-10-01T00:00:00.000Z'),
			       ('plain', 'p', 'db', 'postgres', '', '2026-10-01T00:00:00.000Z')`,
		`INSERT INTO settings (key, value) VALUES ('onepassword.token', 'ops_global'), ('other', 'kept')`,
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
	if l, err := s.OnePasswordLink(ctx, "linked"); err != nil || l.Environment != "env1" || l.Token != "" {
		t.Errorf("migrated link = %+v, %v; want env1 with no token", l, err)
	}
	if _, err := s.OnePasswordLink(ctx, "plain"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unlinked service has a link: %v", err)
	}
	if _, err := s.Setting(ctx, "onepassword.token"); !errors.Is(err, ErrNotFound) {
		t.Errorf("global 1Password token is still saved: %v", err)
	}
	if v, err := s.Setting(ctx, "other"); err != nil || v != "kept" {
		t.Errorf("other setting = %q, %v; want kept", v, err)
	}
	if rows := rawRows(t, s, `SELECT name FROM pragma_table_info('services') WHERE name = 'onepassword_environment'`); len(rows) != 0 {
		t.Error("services.onepassword_environment still exists")
	}
	if _, err := s.Service(ctx, "linked"); err != nil {
		t.Errorf("Service() after migration: %v", err)
	}
}
