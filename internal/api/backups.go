package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
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
func (s *Server) pauseBackups(ctx context.Context, serviceIDs ...string) (resume func(), err error) {
	ctx, cancel := context.WithTimeout(ctx, forgetTimeout)
	defer cancel()
	var resumes []func()
	resume = func() {
		for _, r := range resumes {
			r()
		}
	}
	for _, id := range serviceIDs {
		r, err := s.backups.PauseService(ctx, id)
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
func (s *Server) forgetBackups(ctx context.Context, serviceIDs ...string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), forgetTimeout)
	defer cancel()
	for _, id := range serviceIDs {
		if err := s.backups.ForgetService(ctx, id); err != nil {
			s.log.Error("forget backups of deleted service", "service", id, "err", err)
		}
	}
}

// backupList returns the backups of a service, or of shed.db for an empty
// serviceID, with their download names. serviceName is empty for shed.db.
func (s *Server) backupList(ctx context.Context, serviceID, serviceName string) ([]backupJSON, error) {
	backups, err := s.backups.Backups(ctx, serviceID, backupsPageSize)
	if err != nil {
		return nil, err
	}
	out := make([]backupJSON, 0, len(backups))
	for _, b := range backups {
		out = append(out, toBackup(b, serviceName))
	}
	return out, nil
}

func (s *Server) serviceBackups(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	svc, err := s.store.Service(ctx, r.PathValue("id"))
	if err != nil {
		return err
	}
	policy, err := s.backups.Policy(ctx, svc.ID)
	if err != nil {
		return err
	}
	backups, err := s.backupList(ctx, svc.ID, svc.Name)
	if err != nil {
		return err
	}
	out := serviceBackupsJSON{Policy: toBackupPolicy(policy), Backups: backups}
	switch rs, err := s.store.LatestRestore(ctx, svc.ID); {
	case err == nil:
		v := toRestore(rs)
		out.Restore = &v
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	return writeJSON(w, http.StatusOK, out)
}

func (s *Server) systemBackups(w http.ResponseWriter, r *http.Request) error {
	policy, err := s.backups.Policy(r.Context(), "")
	if err != nil {
		return err
	}
	backups, err := s.backupList(r.Context(), "", "")
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, systemBackupsJSON{Policy: toBackupPolicy(policy), Backups: backups})
}

// putBackupPolicy saves the policy of a service, or of shed.db for an empty
// serviceID.
func (s *Server) putBackupPolicy(w http.ResponseWriter, r *http.Request, serviceID string) error {
	var in backupPolicyInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	policy, err := s.backups.SetPolicy(r.Context(), serviceID, backup.PolicyInput(in))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupPolicy(policy))
}

func (s *Server) putServiceBackupPolicy(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return s.putBackupPolicy(w, r, svc.ID)
}

func (s *Server) putSystemBackupPolicy(w http.ResponseWriter, r *http.Request) error {
	return s.putBackupPolicy(w, r, "")
}

func (s *Server) runServiceBackup(w http.ResponseWriter, r *http.Request) error {
	svc, err := s.store.Service(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	b, err := s.backups.BackUp(r.Context(), svc.ID)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toBackup(b, svc.Name))
}

func (s *Server) runSystemBackup(w http.ResponseWriter, r *http.Request) error {
	b, err := s.backups.BackUp(r.Context(), "")
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toBackup(b, ""))
}

// downloadBackup streams the decrypted, still compressed archive.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	b, err := s.store.Backup(ctx, r.PathValue("id"))
	if err != nil {
		return err
	}
	name, err := s.backupServiceName(ctx, b)
	if err != nil {
		return err
	}
	rc, err := s.backups.Open(ctx, b.ID)
	if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zstd")
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": backup.DownloadName(b, name)}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, rc); err != nil && ctx.Err() == nil {
		// The status is sent; all that is left is to cut the stream short.
		s.log.Error("stream backup", "backup", b.ID, "err", err)
	}
	return nil
}

// backupServiceName returns the name of the service b belongs to, or "" for
// shed.db.
func (s *Server) backupServiceName(ctx context.Context, b store.Backup) (string, error) {
	if b.ServiceID == "" {
		return "", nil
	}
	svc, err := s.store.Service(ctx, b.ServiceID)
	if err != nil {
		return "", err
	}
	return svc.Name, nil
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) error {
	rs, err := s.backups.Restore(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, toRestore(rs))
}

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) error {
	if err := s.backups.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getBackupSettings(w http.ResponseWriter, r *http.Request) error {
	st, err := s.backups.Settings(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupSettings(st))
}

func (s *Server) putBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var in backupSettingsInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.Encryption == nil {
		return errorf(http.StatusBadRequest, "encryption is required")
	}
	input := backup.SettingsInput{Encrypt: in.Encryption.Enabled}
	if in.S3 != nil {
		v := backup.S3Input(*in.S3)
		input.S3 = &v
	}
	st, err := s.backups.SetSettings(r.Context(), input)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, toBackupSettings(st))
}

// testBackupSettings checks the S3 destination of a settings body without
// saving it.
func (s *Server) testBackupSettings(w http.ResponseWriter, r *http.Request) error {
	var in backupSettingsInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	if in.S3 == nil {
		return errorf(http.StatusBadRequest, "there is no S3 destination to test")
	}
	if err := s.backups.TestS3(r.Context(), backup.S3Input(*in.S3)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) getBackupKey(w http.ResponseWriter, r *http.Request) error {
	id, err := s.backups.Identity(r.Context())
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	return writeJSON(w, http.StatusOK, map[string]string{"identity": id})
}

func toBackupSettings(st backup.Settings) backupSettingsJSON {
	var out backupSettingsJSON
	if st.S3 != nil {
		v := s3SettingsJSON(*st.S3)
		out.S3 = &v
	}
	out.Encryption.Enabled = st.Encrypt
	out.Encryption.Recipient = st.Recipient
	return out
}
