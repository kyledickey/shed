package store

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestBackupDestinations(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	if _, err := s.BackupDestination(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("BackupDestination(missing) error = %v, want ErrNotFound", err)
	}
	in := BackupDestination{Endpoint: "https://s3", Region: "eu", Bucket: "b", Prefix: "p", AccessKeyID: "AK", SecretAccessKey: "SK"}
	d, err := s.PutBackupDestination(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == "" || d.CreatedAt.IsZero() || d.Bucket != "b" || d.SecretAccessKey != "SK" {
		t.Fatalf("PutBackupDestination() = %+v", d)
	}

	// The same location keeps its ID and takes the new credentials.
	in.AccessKeyID, in.SecretAccessKey = "AK2", "SK2"
	again, err := s.PutBackupDestination(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != d.ID || again.SecretAccessKey != "SK2" || again.AccessKeyID != "AK2" {
		t.Errorf("PutBackupDestination(same location) = %+v, want ID %s with new credentials", again, d.ID)
	}

	// Another location is another destination, and the first stays.
	in.Prefix = "q"
	other, err := s.PutBackupDestination(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == d.ID {
		t.Error("a new prefix reused the destination")
	}
	if got, err := s.BackupDestination(ctx, d.ID); err != nil || got.Prefix != "p" || got.SecretAccessKey != "SK2" {
		t.Errorf("BackupDestination() = %+v, %v", got, err)
	}

	sv := mustService(t, s, Service{ProjectID: mustProject(t, s, "p").ID, Name: "a", Kind: "postgres"})
	b, err := s.CreateBackup(ctx, Backup{ServiceID: sv.ID, Trigger: BackupManual, Method: MethodDump, Status: BackupSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Backup(ctx, b.ID); got.DestinationID != "" {
		t.Errorf("DestinationID = %q, want empty", got.DestinationID)
	}
	b.RemoteKey, b.DestinationID = "k", other.ID
	if err := s.UpdateBackup(ctx, b); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Backup(ctx, b.ID); got.DestinationID != other.ID {
		t.Errorf("DestinationID = %q, want %s", got.DestinationID, other.ID)
	}
	b.DestinationID = "unknown"
	if err := s.UpdateBackup(ctx, b); err == nil {
		t.Error("UpdateBackup accepted an unknown destination")
	}
}

// migrateTo applies the embedded migrations up to and including version.
func migrateTo(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		v, err := migrationVersion(file)
		if err != nil {
			t.Fatal(err)
		}
		if v > version {
			break
		}
		body, err := migrationFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyMigration(context.Background(), db, v, string(body)); err != nil {
			t.Fatalf("apply %s: %v", file, err)
		}
	}
}

func TestMigrateBackupDestinations(t *testing.T) {
	tests := []struct {
		name     string
		s3       string // backup.s3 setting; "-" = absent
		wantDest bool
	}{
		{"configured", `{"endpoint":"https://s3","region":"eu","bucket":"b","prefix":"p","accessKeyId":"AK","secretAccessKey":"SK","pathStyle":true}`, true},
		{"removed", ``, false},
		{"never set", "-", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "shed.db")
			db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
			if err != nil {
				t.Fatal(err)
			}
			migrateTo(t, db, 9)
			if tt.s3 != "-" {
				if _, err := db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('backup.s3', ?)`, tt.s3); err != nil {
					t.Fatal(err)
				}
			}
			for _, q := range []string{
				`INSERT INTO backups (id, trigger, method, status, remote_key, created_at)
					VALUES ('up', 'manual', 'sqlite', 'succeeded', 'p/system/up.db.zst', '2026-10-04T03:00:00.000Z')`,
				`INSERT INTO backups (id, trigger, method, status, local, created_at)
					VALUES ('local', 'manual', 'sqlite', 'succeeded', 1, '2026-10-04T03:00:00.000Z')`,
			} {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()

			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.Setting(ctx, "backup.s3"); !errors.Is(err, ErrNotFound) {
				t.Errorf("backup.s3 is still set: %v", err)
			}
			id, err := s.Setting(ctx, "backup.destination")
			if !tt.wantDest {
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("backup.destination = %q, %v; want none", id, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			d, err := s.BackupDestination(ctx, id)
			want := BackupDestination{ID: id, Endpoint: "https://s3", Region: "eu", Bucket: "b", Prefix: "p",
				PathStyle: true, AccessKeyID: "AK", SecretAccessKey: "SK", CreatedAt: d.CreatedAt}
			if err != nil || d != want || d.CreatedAt.IsZero() {
				t.Errorf("destination = %+v, %v; want %+v", d, err, want)
			}
			if b, _ := s.Backup(ctx, "up"); b.DestinationID != id {
				t.Errorf("uploaded backup destination = %q, want %s", b.DestinationID, id)
			}
			if b, _ := s.Backup(ctx, "local"); b.DestinationID != "" {
				t.Errorf("local backup destination = %q, want none", b.DestinationID)
			}
		})
	}
}
