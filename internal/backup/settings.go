package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"filippo.io/age"

	"github.com/kyledickey/shed/internal/store"
)

// Settings keys.
const (
	keyS3           = "backup.s3"           // JSON s3Stored; "" = none
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

// s3Stored is the JSON form of the destination in settings.
type s3Stored struct {
	Endpoint        string `json:"endpoint"`
	Region          string `json:"region"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	PathStyle       bool   `json:"pathStyle"`
}

func (s s3Stored) config() S3Config {
	return S3Config{
		Endpoint:        s.Endpoint,
		Region:          s.Region,
		Bucket:          s.Bucket,
		AccessKeyID:     s.AccessKeyID,
		SecretAccessKey: s.SecretAccessKey,
		PathStyle:       s.PathStyle,
	}
}

// key returns the object key of an archive file of a target.
func (s s3Stored) key(serviceID, file string) string {
	if serviceID == "" {
		return path.Join(s.Prefix, "system", file)
	}
	return path.Join(s.Prefix, "services", serviceID, file)
}

// loadS3 returns the stored destination, or nil if there is none.
func (m *Manager) loadS3(ctx context.Context) (*s3Stored, error) {
	v, err := m.store.Setting(ctx, keyS3)
	if errors.Is(err, store.ErrNotFound) || err == nil && strings.TrimSpace(v) == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	var s s3Stored
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return nil, fmt.Errorf("backup: decode %s: %w", keyS3, err)
	}
	return &s, nil
}

// remote returns a client of the configured destination, or nil if there is
// none.
func (m *Manager) remote(ctx context.Context) (Remote, *s3Stored, error) {
	s, err := m.loadS3(ctx)
	if err != nil || s == nil {
		return nil, nil, err
	}
	r, err := m.newRemote(s.config())
	if err != nil {
		return nil, nil, fmt.Errorf("backup: s3: %w", err)
	}
	return r, s, nil
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
	s, err := m.loadS3(ctx)
	if err != nil {
		return Settings{}, err
	}
	if s != nil {
		out.S3 = &S3Settings{
			Endpoint:    s.Endpoint,
			Region:      s.Region,
			Bucket:      s.Bucket,
			Prefix:      s.Prefix,
			AccessKeyID: s.AccessKeyID,
			PathStyle:   s.PathStyle,
			HasSecret:   s.SecretAccessKey != "",
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

// s3FromInput merges in with the stored destination: an empty secret keeps
// the stored one. The result is checked with NewRemote; problems are
// reported with an error wrapping ErrInvalid.
func (m *Manager) s3FromInput(ctx context.Context, in S3Input) (s3Stored, Remote, error) {
	s := s3Stored{
		Endpoint:        strings.TrimSpace(in.Endpoint),
		Region:          strings.TrimSpace(in.Region),
		Bucket:          strings.TrimSpace(in.Bucket),
		Prefix:          strings.Trim(strings.TrimSpace(in.Prefix), "/"),
		AccessKeyID:     strings.TrimSpace(in.AccessKeyID),
		SecretAccessKey: in.SecretAccessKey,
		PathStyle:       in.PathStyle,
	}
	if s.SecretAccessKey == "" {
		old, err := m.loadS3(ctx)
		if err != nil {
			return s3Stored{}, nil, err
		}
		if old != nil {
			s.SecretAccessKey = old.SecretAccessKey
		}
	}
	r, err := m.newRemote(s.config())
	if err != nil {
		return s3Stored{}, nil, invalidf("%v", err)
	}
	return s, r, nil
}

// SetSettings stores the global backup settings. Turning encryption on for
// the first time generates the age identity. Turning it off keeps the
// identity, which older archives still need.
func (m *Manager) SetSettings(ctx context.Context, in SettingsInput) (Settings, error) {
	v := ""
	if in.S3 != nil {
		s, _, err := m.s3FromInput(ctx, *in.S3)
		if err != nil {
			return Settings{}, err
		}
		b, err := json.Marshal(s)
		if err != nil {
			return Settings{}, fmt.Errorf("backup: encode s3 settings: %w", err)
		}
		v = string(b)
	}
	if in.Encrypt {
		if _, err := m.identity(ctx); errors.Is(err, store.ErrNotFound) {
			id, err := age.GenerateX25519Identity()
			if err != nil {
				return Settings{}, fmt.Errorf("backup: generate age identity: %w", err)
			}
			if err := m.store.SetSetting(ctx, keyAgeIdentity, id.String()); err != nil {
				return Settings{}, fmt.Errorf("backup: %w", err)
			}
		} else if err != nil {
			return Settings{}, err
		}
	}
	if err := m.store.SetSetting(ctx, keyS3, v); err != nil {
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
