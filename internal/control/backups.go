package control

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/kyledickey/shed/internal/audit"
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
		p.recordService(ctx, err, "backup.policy.update", serviceID, nil)
		return backup.Policy{}, err
	}
	return p.setBackupPolicy(ctx, svc, in)
}

// SetSystemBackupPolicy saves the backup policy of shed.db.
func (p *Plane) SetSystemBackupPolicy(ctx context.Context, in backup.PolicyInput) (backup.Policy, error) {
	return p.setBackupPolicy(ctx, store.Service{}, in)
}

// setBackupPolicy saves the backup policy of svc, or of shed.db for the zero
// Service.
func (p *Plane) setBackupPolicy(ctx context.Context, svc store.Service, in backup.PolicyInput) (backup.Policy, error) {
	old, oerr := p.backups.Policy(ctx, svc.ID)
	policy, err := p.backups.SetPolicy(ctx, svc.ID, in)
	detail := map[string]any{}
	if oerr == nil {
		detail["fields"] = changedPolicy(old.PolicyInput, in)
	}
	p.record(ctx, err, backupEvent("backup.policy.update", svc, "", detail))
	return policy, err
}

// changedPolicy returns the names of the settings that differ between a and
// b.
func changedPolicy(a, b backup.PolicyInput) []string {
	return changed([]field{
		{"enabled", a.Enabled != b.Enabled},
		{"schedule", a.Schedule != b.Schedule},
		{"compression", a.Compression != b.Compression},
		{"keepLocal", a.KeepLocal != b.KeepLocal},
		{"upload", a.Upload != b.Upload},
		{"keepRemote", a.KeepRemote != b.KeepRemote},
	})
}

// backupEvent returns an event about backup id of svc, or of shed.db for the
// zero Service.
func backupEvent(action string, svc store.Service, id string, detail map[string]any) audit.Event {
	e := serviceEvent(action, svc, detail)
	if svc.ID == "" {
		e.Detail["system"] = true
	}
	e.Target = id
	return e
}

// recordBackup records action on backup b, naming its service as it is
// stored now.
func (p *Plane) recordBackup(ctx context.Context, err error, action string, b store.Backup) {
	svc := store.Service{ID: b.ServiceID}
	if b.ServiceID != "" {
		if s, lerr := p.store.Service(ctx, b.ServiceID); lerr == nil {
			svc = s
		}
	}
	p.record(ctx, err, backupEvent(action, svc, b.ID, nil))
}

// BackUpService starts a backup of a service.
func (p *Plane) BackUpService(ctx context.Context, serviceID string) (BackupView, error) {
	svc, err := p.store.Service(ctx, serviceID)
	if err != nil {
		p.recordService(ctx, err, "backup.run", serviceID, nil)
		return BackupView{}, err
	}
	b, err := p.backups.BackUp(ctx, svc.ID)
	p.record(ctx, err, backupEvent("backup.run", svc, b.ID, nil))
	if err != nil {
		return BackupView{}, err
	}
	return toBackup(b, svc.Name), nil
}

// BackUpSystem starts a backup of shed.db.
func (p *Plane) BackUpSystem(ctx context.Context) (BackupView, error) {
	b, err := p.backups.BackUp(ctx, "")
	p.record(ctx, err, backupEvent("backup.run", store.Service{}, b.ID, nil))
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
		p.recordBackup(ctx, err, "backup.download", store.Backup{ID: id})
		return nil, "", err
	}
	rc, fileName, err = p.openBackup(ctx, b)
	p.recordBackup(ctx, err, "backup.download", b)
	return rc, fileName, err
}

func (p *Plane) openBackup(ctx context.Context, b store.Backup) (io.ReadCloser, string, error) {
	name := ""
	if b.ServiceID != "" {
		svc, err := p.store.Service(ctx, b.ServiceID)
		if err != nil {
			return nil, "", err
		}
		name = svc.Name
	}
	rc, err := p.backups.Open(ctx, b.ID)
	if err != nil {
		return nil, "", err
	}
	return rc, backup.DownloadName(b, name), nil
}

// RestoreBackup starts restoring a backup into its service, or into shed.db.
func (p *Plane) RestoreBackup(ctx context.Context, id string) (store.Restore, error) {
	b, berr := p.store.Backup(ctx, id)
	if berr != nil {
		b = store.Backup{ID: id}
	}
	r, err := p.backups.Restore(ctx, id)
	p.recordBackup(ctx, err, "backup.restore", b)
	return r, err
}

// DeleteBackup deletes a backup's archives and record.
func (p *Plane) DeleteBackup(ctx context.Context, id string) error {
	b, berr := p.store.Backup(ctx, id)
	if berr != nil {
		b = store.Backup{ID: id}
	}
	err := p.backups.Delete(ctx, id)
	p.recordBackup(ctx, err, "backup.delete", b)
	return err
}

// BackupSettings returns the S3 destination and encryption settings.
func (p *Plane) BackupSettings(ctx context.Context) (backup.Settings, error) {
	return p.backups.Settings(ctx)
}

// SetBackupSettings saves the S3 destination and encryption settings.
func (p *Plane) SetBackupSettings(ctx context.Context, in backup.SettingsInput) (backup.Settings, error) {
	old, oerr := p.backups.Settings(ctx)
	out, err := p.backups.SetSettings(ctx, in)
	detail := map[string]any{}
	if oerr == nil {
		detail["fields"] = changedBackupSettings(old, in)
	}
	p.record(ctx, err, audit.Event{Action: "backup.settings.update", Detail: detail})
	return out, err
}

// changedBackupSettings returns the names of the settings in that differ
// from old. A secret access key counts as changed whenever one is given.
func changedBackupSettings(old backup.Settings, in backup.SettingsInput) []string {
	var was backup.S3Settings
	if old.S3 != nil {
		was = *old.S3
	}
	var now backup.S3Input
	if in.S3 != nil {
		now = *in.S3
	}
	return changed([]field{
		{"encrypt", old.Encrypt != in.Encrypt},
		{"s3", (old.S3 == nil) != (in.S3 == nil)},
		{"endpoint", was.Endpoint != now.Endpoint},
		{"region", was.Region != now.Region},
		{"bucket", was.Bucket != now.Bucket},
		{"prefix", was.Prefix != now.Prefix},
		{"accessKeyId", was.AccessKeyID != now.AccessKeyID},
		{"secretAccessKey", now.SecretAccessKey != ""},
		{"pathStyle", was.PathStyle != now.PathStyle},
	})
}

// TestBackupS3 checks that an S3 destination can be written, without saving
// it.
func (p *Plane) TestBackupS3(ctx context.Context, in backup.S3Input) error {
	return p.backups.TestS3(ctx, in)
}

// BackupIdentity returns the age identity that decrypts backups.
func (p *Plane) BackupIdentity(ctx context.Context) (string, error) {
	id, err := p.backups.Identity(ctx)
	p.record(ctx, err, audit.Event{Action: "backup.key.read"})
	return id, err
}
