package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrate applies, in order, every embedded migration newer than the
// database's PRAGMA user_version. Migration files are named NNN_name.sql, and
// each runs in its own transaction. A database migrated past the newest
// embedded migration, by a newer shed, is refused.
func migrate(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("store: list migrations: %w", err)
	}
	if len(files) > 0 {
		latest, err := migrationVersion(files[len(files)-1])
		if err != nil {
			return err
		}
		if current > latest {
			return fmt.Errorf("store: database schema version %d is newer than this shed supports (%d); "+
				"run a newer shed, or restore a backup of shed.db from before the upgrade", current, latest)
		}
	}
	for _, file := range files { // Glob returns sorted names.
		version, err := migrationVersion(file)
		if err != nil {
			return err
		}
		if version <= current {
			continue
		}
		body, err := migrationFS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", file, err)
		}
		if err := applyMigration(ctx, db, version, string(body)); err != nil {
			return fmt.Errorf("store: apply %s: %w", file, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, body); err != nil {
		return err
	}
	// PRAGMA statements cannot take bound parameters.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

// migrationVersion extracts the numeric prefix of a migration file name.
func migrationVersion(file string) (int, error) {
	name := file[strings.LastIndex(file, "/")+1:]
	prefix, _, _ := strings.Cut(name, "_")
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("store: migration %s has no numeric prefix", file)
	}
	return version, nil
}
