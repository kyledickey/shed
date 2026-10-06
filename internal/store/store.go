// Package store persists shed's state in SQLite.
//
// It exposes plain structs and hand-written queries over database/sql, using
// the pure-Go modernc.org/sqlite driver. The schema is created and upgraded by
// the numbered migrations embedded in the package.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var (
	// ErrNotFound is returned when a requested row does not exist.
	ErrNotFound = errors.New("store: not found")

	// ErrConflict is returned when a write violates a uniqueness constraint,
	// such as a duplicate project name or domain host.
	ErrConflict = errors.New("store: conflict")

	// ErrWrongKey is returned by [Open] when the database was encrypted with
	// a different key.
	ErrWrongKey = errors.New("store: database is encrypted with a different key")
)

// Store is a handle to the SQLite database. It is safe for concurrent use.
type Store struct {
	db    *sql.DB
	crypt *crypter
}

// Open opens the database at path, creating it and applying any pending
// migrations. Settings values, service variables, and backup destination
// secret keys are encrypted at rest with key, which must be [KeySize] bytes;
// Open encrypts them in a database that predates encryption. It returns
// ErrWrongKey if the database was encrypted with a different key.
func Open(path string, key []byte) (*Store, error) {
	c, err := newCrypter(key)
	if err != nil {
		return nil, err
	}
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// A single connection serializes access and rules out SQLITE_BUSY between
	// our own goroutines. The load is far too small for this to matter.
	db.SetMaxOpenConns(1)
	if err := migrate(context.Background(), db); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureEncrypted(context.Background(), db, c); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	return &Store{db: db, crypt: c}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// NewID returns a random 12-character lowercase base32 identifier.
func NewID() string {
	return strings.ToLower(rand.Text()[:12])
}

// timeLayout is a fixed-width RFC 3339 UTC layout, so that stored times sort
// lexicographically.
const timeLayout = "2006-01-02T15:04:05.000Z"

// now returns the current UTC time at the precision the store keeps.
func now() time.Time {
	return time.Now().UTC().Truncate(time.Millisecond)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// timestamp scans a stored time into a time.Time.
type timestamp time.Time

// Scan implements [sql.Scanner].
func (t *timestamp) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("store: scan time from %T", src)
	}
	parsed, err := time.Parse(timeLayout, s)
	if err != nil {
		return fmt.Errorf("store: parse time: %w", err)
	}
	*t = timestamp(parsed)
	return nil
}

// nullTimestamp scans a nullable stored time into a *time.Time.
type nullTimestamp struct{ dst **time.Time }

// Scan implements [sql.Scanner].
func (n nullTimestamp) Scan(src any) error {
	if src == nil {
		*n.dst = nil
		return nil
	}
	var t timestamp
	if err := t.Scan(src); err != nil {
		return err
	}
	parsed := time.Time(t)
	*n.dst = &parsed
	return nil
}

// optionalTime returns the stored form of t, or nil for a nil time.
func optionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// scanner is implemented by [sql.Row] and [sql.Rows].
type scanner interface {
	Scan(dest ...any) error
}

// queryOne runs a query expected to return one row, scanned by scan. It
// returns ErrNotFound if there is none.
func queryOne[T any](ctx context.Context, s *Store, scan func(scanner) (T, error), query string, args ...any) (T, error) {
	v, err := scan(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

// queryAll runs a query and scans every row with scan.
func queryAll[T any](ctx context.Context, s *Store, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// exec runs a statement that need not affect any rows.
func (s *Store) exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return mapError(err)
}

// execOne runs a statement that must affect a row, returning ErrNotFound
// otherwise.
func (s *Store) execOne(ctx context.Context, query string, args ...any) error {
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return mapError(err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// mapError translates uniqueness violations into ErrConflict.
func mapError(err error) error {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		switch serr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %v", ErrConflict, err)
		}
	}
	return err
}
