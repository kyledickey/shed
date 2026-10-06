package control

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/kyledickey/shed/internal/backup"
	"github.com/kyledickey/shed/internal/store"
)

// backupsPageSize is how many backups a service or shed.db lists.
const backupsPageSize = 100

// forgetTimeout bounds how long deleting a service waits for its running
// backup to stop.
const forgetTimeout = 30 * time.Second

// pauseBackups stops the backups and restores of services from running until
// the returned function is called, so that deleting them does not find their
// volumes in use. It returns backup.ErrBusy, pausing nothing, if a restore of
// one of them is running.
func (p *Plane) pauseBackups(ctx context.Context, serviceIDs ...string) (resume func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, forgetTimeout)
	defer cancel()
	var resumes []func()
	resume = func() {
		for _, r := range resumes {
			r()
		}
	}
	for _, id := range serviceIDs {
		r, err := p.backups.PauseService(ctx, id)
		if err != nil {
			resume()
			return nil, err
		}
		resumes = append(resumes, r)
	}
	return resume, nil
}

// forgetBackups tells the backup manager that services were deleted. A
// failure does not undo the deletion, so it is only logged.
func (p *Plane) forgetBackups(ctx context.Context, serviceIDs ...string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), forgetTimeout)
	defer cancel()
	for _, id := range serviceIDs {
		if err := p.backups.ForgetService(ctx, id); err != nil {
			p.log.Error("forget backups of deleted service", "service", id, "err", err)
		}
	}
}

// backupList returns the policy and newest backups of a service, or of
// shed.db for an empty serviceID. serviceName is empty for shed.db.
func (p *Plane) backupList(ctx context.Context, serviceID, serviceName string) (BackupList, error) {
	policy, err := p.backups.Policy(ctx, serviceID)
	if err != nil {
		return BackupList{}, err
	}
	backups, err := p.backups.Backups(ctx, serviceID, backupsPageSize)
	if err != nil {
		return BackupList{}, err
	}
	out := BackupList{Policy: policy, Backups: make([]BackupView, 0, len(backups))}
	for _, b := range backups {
		out.Backups = append(out.Backups, toBackup(b, serviceName))
	}
	return out, nil
}

// toBackup adds b's download name. serviceName is empty for shed.db.
func toBackup(b store.Backup, serviceName string) BackupView {
	return BackupView{Backup: b, FileName: backup.DownloadName(b, serviceName)}
}

// ServiceBackups returns a service's backup policy, its newest backups, and
// its latest restore.
func (p *Plane) ServiceBackups(ctx context.Context, serviceID string) (BackupList, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return BackupList{}, err
	}
	out, err := p.backupList(ctx, svc.ID, svc.Name)
	if err != nil {
		return BackupList{}, err
	}
	switch rs, err := p.store.LatestRestore(ctx, svc.ID); {
	case err == nil:
		out.Restore = &rs
	case !errors.Is(err, store.ErrNotFound):
		return BackupList{}, err
	}
	return out, nil
}

// SystemBackups returns the backup policy and newest backups of shed.db.
func (p *Plane) SystemBackups(ctx context.Context) (BackupList, error) {
	return p.backupList(ctx, "", "")
}

// SetServiceBackupPolicy saves a service's backup policy.
func (p *Plane) SetServiceBackupPolicy(ctx context.Context, serviceID string, in backup.PolicyInput) (backup.Policy, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return backup.Policy{}, err
	}
	return p.backups.SetPolicy(ctx, svc.ID, in)
}

// SetSystemBackupPolicy saves the backup policy of shed.db.
func (p *Plane) SetSystemBackupPolicy(ctx context.Context, in backup.PolicyInput) (backup.Policy, error) {
	return p.backups.SetPolicy(ctx, "", in)
}

// BackUpService starts a backup of a service.
func (p *Plane) BackUpService(ctx context.Context, serviceID string) (BackupView, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		return BackupView{}, err
	}
	b, err := p.backups.BackUp(ctx, svc.ID)
	if err != nil {
		return BackupView{}, err
	}
	return toBackup(b, svc.Name), nil
}

// BackUpSystem starts a backup of shed.db.
func (p *Plane) BackUpSystem(ctx context.Context) (BackupView, error) {
	b, err := p.backups.BackUp(ctx, "")
	if err != nil {
		return BackupView{}, err
	}
	return toBackup(b, ""), nil
}

// OpenBackup opens a backup's archive, decrypted but still compressed, and
// returns the name it downloads as. The caller closes it.
func (p *Plane) OpenBackup(ctx context.Context, id string) (rc io.ReadCloser, fileName string, err error) {
	b, err := p.store.Backup(ctx, id)
	if err != nil {
		return nil, "", err
	}
	name := ""
	if b.ServiceID != "" {
		svc, err := p.store.Service(ctx, b.ServiceID)
		if err != nil {
			return nil, "", err
		}
		name = svc.Name
	}
	rc, err = p.backups.Open(ctx, b.ID)
	if err != nil {
		return nil, "", err
	}
	return rc, backup.DownloadName(b, name), nil
}

// RestoreBackup starts restoring a backup into its service, or into shed.db.
func (p *Plane) RestoreBackup(ctx context.Context, id string) (store.Restore, error) {
	return p.backups.Restore(ctx, id)
}

// DeleteBackup deletes a backup's archives and record.
func (p *Plane) DeleteBackup(ctx context.Context, id string) error {
	return p.backups.Delete(ctx, id)
}

// BackupSettings returns the S3 destination and encryption settings.
func (p *Plane) BackupSettings(ctx context.Context) (backup.Settings, error) {
	return p.backups.Settings(ctx)
}

// SetBackupSettings saves the S3 destination and encryption settings.
func (p *Plane) SetBackupSettings(ctx context.Context, in backup.SettingsInput) (backup.Settings, error) {
	return p.backups.SetSettings(ctx, in)
}

// TestBackupS3 checks that an S3 destination can be written, without saving
// it.
func (p *Plane) TestBackupS3(ctx context.Context, in backup.S3Input) error {
	return p.backups.TestS3(ctx, in)
}

// BackupIdentity returns the age identity that decrypts backups.
func (p *Plane) BackupIdentity(ctx context.Context) (string, error) {
	return p.backups.Identity(ctx)
}
