package backup

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"filippo.io/age"

	"github.com/kyledickey/shed/internal/store"
)

// Settings keys.
const (
	keyDestination  = "backup.destination"  // ID of the current destination; "" = none
	keyEncrypt      = "backup.encrypt"      // "true" or "false"
	keyAgeIdentity  = "backup.age_identity" // AGE-SECRET-KEY-1...
	keySystemPolicy = "backup.system"       // JSON systemPolicy
)

// S3Config is what NewRemote needs to reach a bucket.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool
}

// S3Settings describes the S3 destination, without its secret.
type S3Settings struct {
	Endpoint    string
	Region      string
	Bucket      string
	Prefix      string
	AccessKeyID string
	PathStyle   bool
	// HasSecret reports whether a secret access key is stored.
	HasSecret bool
}

// Settings are the global backup settings.
type Settings struct {
	// S3 is nil when no destination is configured.
	S3 *S3Settings
	// Encrypt reports whether new archives are encrypted.
	Encrypt bool
	// Recipient is the age public key of the stored identity, or "" if there
	// is none yet.
	Recipient string
}

// S3Input sets the S3 destination. An empty SecretAccessKey keeps the stored
// one.
type S3Input struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool
}

// SettingsInput changes the global backup settings.
type SettingsInput struct {
	// S3 nil removes the destination.
	S3      *S3Input
	Encrypt bool
}

// s3Config returns what NewRemote needs to reach d.
func s3Config(d store.BackupDestination) S3Config {
	return S3Config{
		Endpoint:        d.Endpoint,
		Region:          d.Region,
		Bucket:          d.Bucket,
		AccessKeyID:     d.AccessKeyID,
		SecretAccessKey: d.SecretAccessKey,
		PathStyle:       d.PathStyle,
	}
}

// objectKey returns the key in d of an archive file of a target.
func objectKey(d store.BackupDestination, serviceID, file string) string {
	if serviceID == "" {
		return path.Join(d.Prefix, "system", file)
	}
	return path.Join(d.Prefix, "services", serviceID, file)
}

// destination returns the current destination, or nil if there is none.
func (m *Manager) destination(ctx context.Context) (*store.BackupDestination, error) {
	id, err := m.store.Setting(ctx, keyDestination)
	if errors.Is(err, store.ErrNotFound) || err == nil && id == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	d, err := m.store.BackupDestination(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	return &d, nil
}

// remote returns a client of the current destination, or nil if there is
// none.
func (m *Manager) remote(ctx context.Context) (Remote, *store.BackupDestination, error) {
	d, err := m.destination(ctx)
	if err != nil || d == nil {
		return nil, nil, err
	}
	r, err := m.newRemote(s3Config(*d))
	if err != nil {
		return nil, nil, fmt.Errorf("backup: s3: %w", err)
	}
	return r, d, nil
}

// errNoDestination is a remote object whose destination is unknown.
var errNoDestination = invalidError{"the backup's S3 destination is unknown"}

// remoteOf returns a client of the destination that b was uploaded to,
// whatever the current destination is. It returns errNoDestination if b
// does not record one.
func (m *Manager) remoteOf(ctx context.Context, b store.Backup) (Remote, error) {
	if b.DestinationID == "" {
		return nil, errNoDestination
	}
	d, err := m.store.BackupDestination(ctx, b.DestinationID)
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	r, err := m.newRemote(s3Config(d))
	if err != nil {
		return nil, fmt.Errorf("backup: s3: %w", err)
	}
	return r, nil
}

// identity returns the stored age identity, or store.ErrNotFound.
func (m *Manager) identity(ctx context.Context) (*age.X25519Identity, error) {
	v, err := m.store.Setting(ctx, keyAgeIdentity)
	if err != nil {
		return nil, fmt.Errorf("backup: age identity: %w", err)
	}
	id, err := age.ParseX25519Identity(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("backup: parse age identity: %w", err)
	}
	return id, nil
}

// ensureIdentity generates and stores the age identity if there is none. It
// never replaces a stored identity: when concurrent calls race, the first
// write wins and the others keep it.
func (m *Manager) ensureIdentity(ctx context.Context) error {
	_, err := m.identity(ctx)
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return fmt.Errorf("backup: generate age identity: %w", err)
	}
	if _, err := m.store.AddSetting(ctx, keyAgeIdentity, id.String()); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	_, err = m.identity(ctx)
	return err
}

// recipient returns the recipient new archives are encrypted to, or nil
// while encryption is off.
func (m *Manager) recipient(ctx context.Context) (age.Recipient, error) {
	v, err := m.store.Setting(ctx, keyEncrypt)
	if errors.Is(err, store.ErrNotFound) || err == nil && v != "true" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	id, err := m.identity(ctx)
	if err != nil {
		return nil, err
	}
	return id.Recipient(), nil
}

// Settings returns the global backup settings.
func (m *Manager) Settings(ctx context.Context) (Settings, error) {
	var out Settings
	d, err := m.destination(ctx)
	if err != nil {
		return Settings{}, err
	}
	if d != nil {
		out.S3 = &S3Settings{
			Endpoint:    d.Endpoint,
			Region:      d.Region,
			Bucket:      d.Bucket,
			Prefix:      d.Prefix,
			AccessKeyID: d.AccessKeyID,
			PathStyle:   d.PathStyle,
			HasSecret:   d.SecretAccessKey != "",
		}
	}
	v, err := m.store.Setting(ctx, keyEncrypt)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Settings{}, fmt.Errorf("backup: %w", err)
	}
	out.Encrypt = v == "true"
	id, err := m.identity(ctx)
	switch {
	case err == nil:
		out.Recipient = id.Recipient().String()
	case !errors.Is(err, store.ErrNotFound):
		return Settings{}, err
	}
	return out, nil
}

// s3FromInput merges in with the current destination: an empty secret keeps
// the stored one. The result is checked with NewRemote; problems are
// reported with an error wrapping ErrInvalid.
func (m *Manager) s3FromInput(ctx context.Context, in S3Input) (store.BackupDestination, Remote, error) {
	s := store.BackupDestination{
		Endpoint:        strings.TrimSpace(in.Endpoint),
		Region:          strings.TrimSpace(in.Region),
		Bucket:          strings.TrimSpace(in.Bucket),
		Prefix:          strings.Trim(strings.TrimSpace(in.Prefix), "/"),
		AccessKeyID:     strings.TrimSpace(in.AccessKeyID),
		SecretAccessKey: in.SecretAccessKey,
		PathStyle:       in.PathStyle,
	}
	if s.SecretAccessKey == "" {
		old, err := m.destination(ctx)
		if err != nil {
			return store.BackupDestination{}, nil, err
		}
		if old != nil {
			s.SecretAccessKey = old.SecretAccessKey
		}
	}
	r, err := m.newRemote(s3Config(s))
	if err != nil {
		return store.BackupDestination{}, nil, invalidf("%v", err)
	}
	return s, r, nil
}

// SetSettings stores the global backup settings. Turning encryption on for
// the first time generates the age identity. Turning it off keeps the
// identity, which older archives still need. A destination at a new location
// is stored as a new one, so that backups already uploaded keep reading from
// and deleting in the destination they were uploaded to.
func (m *Manager) SetSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	v := ""
	if in.S3 != nil {
		s, _, err := m.s3FromInput(ctx, *in.S3)
		if err != nil {
			return Settings{}, err
		}
		d, err := m.store.PutBackupDestination(ctx, s)
		if err != nil {
			return Settings{}, fmt.Errorf("backup: %w", err)
		}
		v = d.ID
	}
	if in.Encrypt {
		if err := m.ensureIdentity(ctx); err != nil {
			return Settings{}, err
		}
	}
	if err := m.store.SetSetting(ctx, keyDestination, v); err != nil {
		return Settings{}, fmt.Errorf("backup: %w", err)
	}
	if err := m.store.SetSetting(ctx, keyEncrypt, fmt.Sprint(in.Encrypt)); err != nil {
		return Settings{}, fmt.Errorf("backup: %w", err)
	}
	return m.Settings(ctx)
}

// TestS3 checks that the destination in in works by writing, reading, and
// deleting a probe object. An empty secret uses the stored one. Every
// failure wraps ErrInvalid and describes the problem.
func (m *Manager) TestS3(ctx context.Context, in S3Input) error {
	s, r, err := m.s3FromInput(ctx, in)
	if err != nil {
		return err
	}
	if err := r.Check(ctx, path.Join(s.Prefix, ".shed-check-"+store.NewID())); err != nil {
		return invalidf("%v", err)
	}
	return nil
}

// Identity returns the age secret key that encrypted archives are encrypted
// to, or an error wrapping store.ErrNotFound if encryption was never turned
// on.
func (m *Manager) Identity(ctx context.Context) (string, error) {
	id, err := m.identity(ctx)
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
