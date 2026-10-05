package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const backupPolicyCols = `service_id, enabled, schedule, compression, keep_local, upload, keep_remote`

func scanBackupPolicy(r scanner) (BackupPolicy, error) {
	var p BackupPolicy
	err := r.Scan(&p.ServiceID, &p.Enabled, &p.Schedule, &p.Compression, &p.KeepLocal, &p.Upload, &p.KeepRemote)
	return p, err
}

// BackupPolicy returns a service's backup policy, or ErrNotFound if it has
// none stored.
func (s *Store) BackupPolicy(ctx context.Context, serviceID string) (BackupPolicy, error) {
	p, err := queryOne(ctx, s, scanBackupPolicy,
		`SELECT `+backupPolicyCols+` FROM backup_policies WHERE service_id = ?`, serviceID)
	if err != nil {
		return BackupPolicy{}, fmt.Errorf("store: backup policy of service %s: %w", serviceID, err)
	}
	return p, nil
}

// PutBackupPolicy stores p, replacing any existing policy of p.ServiceID.
func (s *Store) PutBackupPolicy(ctx context.Context, p BackupPolicy) error {
	err := s.exec(ctx, `INSERT INTO backup_policies (`+backupPolicyCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (service_id) DO UPDATE SET enabled = excluded.enabled,
			schedule = excluded.schedule, compression = excluded.compression,
			keep_local = excluded.keep_local, upload = excluded.upload,
			keep_remote = excluded.keep_remote`,
		p.ServiceID, p.Enabled, p.Schedule, p.Compression, p.KeepLocal, p.Upload, p.KeepRemote)
	if err != nil {
		return fmt.Errorf("store: put backup policy of service %s: %w", p.ServiceID, err)
	}
	return nil
}

const backupCols = `id, service_id, trigger, method, status, file, size, encrypted, local,
	remote_key, destination_id, remote_error, error, created_at, started_at, finished_at`

func scanBackup(r scanner) (Backup, error) {
	var b Backup
	var serviceID, destinationID sql.NullString
	err := r.Scan(&b.ID, &serviceID, &b.Trigger, &b.Method, &b.Status, &b.File, &b.Size,
		&b.Encrypted, &b.Local, &b.RemoteKey, &destinationID, &b.RemoteError, &b.Error,
		(*timestamp)(&b.CreatedAt), nullTimestamp{&b.StartedAt}, nullTimestamp{&b.FinishedAt})
	b.ServiceID, b.DestinationID = serviceID.String, destinationID.String
	return b, err
}

// optionalString returns the stored form of v, or nil for an empty string.
func optionalString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// CreateBackup stores b. It assigns a new ID and the current time if b.ID and
// b.CreatedAt are unset, and returns b with them filled in. An empty
// b.ServiceID stores a backup of shed.db.
func (s *Store) CreateBackup(ctx context.Context, b Backup) (Backup, error) {
	if b.ID == "" {
		b.ID = NewID()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now()
	}
	err := s.exec(ctx, `INSERT INTO backups (`+backupCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, optionalString(b.ServiceID), b.Trigger, b.Method, b.Status, b.File, b.Size,
		b.Encrypted, b.Local, b.RemoteKey, optionalString(b.DestinationID), b.RemoteError, b.Error, formatTime(b.CreatedAt),
		optionalTime(b.StartedAt), optionalTime(b.FinishedAt))
	if err != nil {
		return Backup{}, fmt.Errorf("store: create backup for service %q: %w", b.ServiceID, err)
	}
	return b, nil
}

// UpdateBackup overwrites the mutable fields of the backup b.ID: its method,
// status, archive details, remote object, errors, and start and finish
// times. It returns ErrNotFound for an unknown backup.
func (s *Store) UpdateBackup(ctx context.Context, b Backup) error {
	err := s.execOne(ctx, `UPDATE backups SET method = ?, status = ?, file = ?, size = ?,
		encrypted = ?, local = ?, remote_key = ?, destination_id = ?, remote_error = ?, error = ?,
		started_at = ?, finished_at = ?
		WHERE id = ?`,
		b.Method, b.Status, b.File, b.Size, b.Encrypted, b.Local, b.RemoteKey, optionalString(b.DestinationID),
		b.RemoteError, b.Error, optionalTime(b.StartedAt), optionalTime(b.FinishedAt), b.ID)
	if err != nil {
		return fmt.Errorf("store: update backup %s: %w", b.ID, err)
	}
	return nil
}

// Backup returns the backup with the given ID, or ErrNotFound.
func (s *Store) Backup(ctx context.Context, id string) (Backup, error) {
	b, err := queryOne(ctx, s, scanBackup, `SELECT `+backupCols+` FROM backups WHERE id = ?`, id)
	if err != nil {
		return Backup{}, fmt.Errorf("store: backup %s: %w", id, err)
	}
	return b, nil
}

// Backups returns a service's backups, newest first. An empty serviceID
// selects the backups of shed.db. A limit of zero or less returns all of them.
func (s *Store) Backups(ctx context.Context, serviceID string, limit int) ([]Backup, error) {
	if limit <= 0 {
		limit = -1 // SQLite: no limit.
	}
	bs, err := queryAll(ctx, s, scanBackup, `SELECT `+backupCols+`
		FROM backups WHERE service_id IS ?
		ORDER BY created_at DESC, rowid DESC LIMIT ?`, optionalString(serviceID), limit)
	if err != nil {
		return nil, fmt.Errorf("store: backups of service %q: %w", serviceID, err)
	}
	return bs, nil
}

// DeleteBackup deletes a backup record. Deleting an unknown backup is not an
// error.
func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	if err := s.exec(ctx, `DELETE FROM backups WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete backup %s: %w", id, err)
	}
	return nil
}

// DeleteFailedBackupsBefore deletes the failed backups created before t and
// returns how many it deleted.
func (s *Store) DeleteFailedBackupsBefore(ctx context.Context, t time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backups WHERE status = ? AND created_at < ?`,
		BackupFailed, formatTime(t))
	if err != nil {
		return 0, fmt.Errorf("store: delete failed backups: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete failed backups: %w", err)
	}
	return n, nil
}

const backupDestinationCols = `id, endpoint, region, bucket, prefix, path_style,
	access_key_id, secret_access_key, created_at`

func scanBackupDestination(r scanner) (BackupDestination, error) {
	var d BackupDestination
	err := r.Scan(&d.ID, &d.Endpoint, &d.Region, &d.Bucket, &d.Prefix, &d.PathStyle,
		&d.AccessKeyID, &d.SecretAccessKey, (*timestamp)(&d.CreatedAt))
	return d, err
}

// PutBackupDestination stores the destination at d's location (endpoint,
// region, bucket, prefix, and path style) and returns it. If one is already
// stored there, its credentials are replaced and its ID kept; otherwise a new
// one is created. d.ID and d.CreatedAt are ignored.
func (s *Store) PutBackupDestination(ctx context.Context, d BackupDestination) (BackupDestination, error) {
	out, err := queryOne(ctx, s, scanBackupDestination, `INSERT INTO backup_destinations
		(`+backupDestinationCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (endpoint, region, bucket, prefix, path_style) DO UPDATE SET
			access_key_id = excluded.access_key_id, secret_access_key = excluded.secret_access_key
		RETURNING `+backupDestinationCols,
		NewID(), d.Endpoint, d.Region, d.Bucket, d.Prefix, d.PathStyle, d.AccessKeyID, d.SecretAccessKey,
		formatTime(now()))
	if err != nil {
		return BackupDestination{}, fmt.Errorf("store: put backup destination %s/%s: %w", d.Endpoint, d.Bucket, err)
	}
	return out, nil
}

// BackupDestination returns the backup destination with the given ID, or
// ErrNotFound.
func (s *Store) BackupDestination(ctx context.Context, id string) (BackupDestination, error) {
	d, err := queryOne(ctx, s, scanBackupDestination,
		`SELECT `+backupDestinationCols+` FROM backup_destinations WHERE id = ?`, id)
	if err != nil {
		return BackupDestination{}, fmt.Errorf("store: backup destination %s: %w", id, err)
	}
	return d, nil
}

// UploadingBackups returns every backup whose status is uploading.
func (s *Store) UploadingBackups(ctx context.Context) ([]Backup, error) {
	bs, err := queryAll(ctx, s, scanBackup, `SELECT `+backupCols+` FROM backups WHERE status = ?`, BackupUploading)
	if err != nil {
		return nil, fmt.Errorf("store: uploading backups: %w", err)
	}
	return bs, nil
}

// FailInterruptedBackups marks every queued or running backup as failed with
// the error msg, and every uploading one, whose archive is complete, as
// succeeded with the upload error msg. It returns how many it changed.
func (s *Store) FailInterruptedBackups(ctx context.Context, msg string) (int64, error) {
	t := formatTime(now())
	res, err := s.db.ExecContext(ctx, `UPDATE backups SET
		status = CASE status WHEN ? THEN ? ELSE ? END,
		error = CASE status WHEN ? THEN error ELSE ? END,
		remote_error = CASE status WHEN ? THEN ? ELSE remote_error END,
		finished_at = ?
		WHERE status IN (?, ?, ?)`,
		BackupUploading, BackupSucceeded, BackupFailed,
		BackupUploading, msg,
		BackupUploading, msg,
		t, BackupQueued, BackupRunning, BackupUploading)
	if err != nil {
		return 0, fmt.Errorf("store: fail interrupted backups: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: fail interrupted backups: %w", err)
	}
	return n, nil
}

const restoreCols = `id, service_id, backup_id, status, error, created_at, finished_at`

func scanRestore(r scanner) (Restore, error) {
	var x Restore
	err := r.Scan(&x.ID, &x.ServiceID, &x.BackupID, &x.Status, &x.Error,
		(*timestamp)(&x.CreatedAt), nullTimestamp{&x.FinishedAt})
	return x, err
}

// CreateRestore stores r. It assigns a new ID and the current time if r.ID and
// r.CreatedAt are unset, and returns r with them filled in.
func (s *Store) CreateRestore(ctx context.Context, r Restore) (Restore, error) {
	if r.ID == "" {
		r.ID = NewID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now()
	}
	err := s.exec(ctx, `INSERT INTO restores (`+restoreCols+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ServiceID, r.BackupID, r.Status, r.Error, formatTime(r.CreatedAt), optionalTime(r.FinishedAt))
	if err != nil {
		return Restore{}, fmt.Errorf("store: create restore for service %s: %w", r.ServiceID, err)
	}
	return r, nil
}

// UpdateRestore overwrites the mutable fields of the restore r.ID: its status,
// error, and finish time. It returns ErrNotFound for an unknown restore.
func (s *Store) UpdateRestore(ctx context.Context, r Restore) error {
	err := s.execOne(ctx, `UPDATE restores SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		r.Status, r.Error, optionalTime(r.FinishedAt), r.ID)
	if err != nil {
		return fmt.Errorf("store: update restore %s: %w", r.ID, err)
	}
	return nil
}

// LatestRestore returns the most recently created restore of a service, or
// ErrNotFound if there is none.
func (s *Store) LatestRestore(ctx context.Context, serviceID string) (Restore, error) {
	r, err := queryOne(ctx, s, scanRestore, `SELECT `+restoreCols+`
		FROM restores WHERE service_id = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, serviceID)
	if err != nil {
		return Restore{}, fmt.Errorf("store: latest restore of service %s: %w", serviceID, err)
	}
	return r, nil
}

// FailInterruptedRestores marks every running restore as failed with the
// error msg, and returns how many it changed.
func (s *Store) FailInterruptedRestores(ctx context.Context, msg string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE restores SET status = ?, error = ?, finished_at = ?
		WHERE status = ?`, RestoreFailed, msg, formatTime(now()), RestoreRunning)
	if err != nil {
		return 0, fmt.Errorf("store: fail interrupted restores: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: fail interrupted restores: %w", err)
	}
	return n, nil
}

// ServicesWithVolumes returns the services that have at least one volume,
// oldest first.
func (s *Store) ServicesWithVolumes(ctx context.Context) ([]Service, error) {
	svs, err := queryAll(ctx, s, scanService, `SELECT `+serviceCols+`
		FROM services WHERE EXISTS (SELECT 1 FROM volumes WHERE volumes.service_id = services.id)
		ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("store: list services with volumes: %w", err)
	}
	return svs, nil
}

// Snapshot writes a consistent copy of the database to path, which must not
// exist.
func (s *Store) Snapshot(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("store: snapshot to %s: %w", path, err)
	}
	return nil
}
