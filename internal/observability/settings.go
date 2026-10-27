package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kyledickey/shed/internal/store"
)

// settingsKey is the settings row holding [Settings] as JSON.
const settingsKey = "telemetry"

// passwordKey is the settings row holding the Grafana admin password.
const passwordKey = "grafana_admin_password"

// Caps limit what one service may send to the gateway.
type Caps struct {
	SpansPerSecond int `json:"spansPerSecond"`
	LogsPerSecond  int `json:"logsPerSecond"`
	// Series is the number of distinct series in the last hour.
	Series int `json:"series"`
}

// Settings are the operator's telemetry choices.
type Settings struct {
	Enabled              bool `json:"enabled"`
	MetricsRetentionDays int  `json:"metricsRetentionDays"`
	LogsRetentionDays    int  `json:"logsRetentionDays"`
	TracesRetentionDays  int  `json:"tracesRetentionDays"`
	// DiskLimitGB bounds the disk VictoriaLogs and VictoriaTraces use
	// together: 60% for logs, 40% for traces.
	DiskLimitGB int `json:"diskLimitGB"`
	// DefaultCaps apply to every service without an override.
	DefaultCaps Caps `json:"defaultCaps"`
}

// DefaultSettings returns the settings of a server that never saved any.
func DefaultSettings() Settings {
	return Settings{
		MetricsRetentionDays: 30,
		LogsRetentionDays:    14,
		TracesRetentionDays:  14,
		DiskLimitGB:          20,
		DefaultCaps:          Caps{SpansPerSecond: 5000, LogsPerSecond: 2000, Series: 50000},
	}
}

// MaxRetentionDays bounds each signal's retention.
const MaxRetentionDays = 3650

// ErrInvalid reports settings out of bounds. Errors wrapping it read as a
// message fit to show.
var ErrInvalid = errors.New("invalid telemetry settings")

// Validate reports the first setting out of bounds.
func (s Settings) Validate() error {
	for _, r := range []struct {
		name string
		days int
	}{
		{"metrics", s.MetricsRetentionDays},
		{"logs", s.LogsRetentionDays},
		{"traces", s.TracesRetentionDays},
	} {
		if r.days < 1 || r.days > MaxRetentionDays {
			return fmt.Errorf("%w: %s retention must be 1 to %d days", ErrInvalid, r.name, MaxRetentionDays)
		}
	}
	if s.DiskLimitGB < 1 {
		return fmt.Errorf("%w: the disk limit must be at least 1 GB", ErrInvalid)
	}
	c := s.DefaultCaps
	if c.SpansPerSecond < 1 || c.LogsPerSecond < 1 || c.Series < 1 {
		return fmt.Errorf("%w: every cap must be at least 1", ErrInvalid)
	}
	return nil
}

// Settings returns the stored settings, or the defaults.
func (m *Manager) Settings(ctx context.Context) (Settings, error) {
	s := DefaultSettings()
	raw, err := m.cfg.Store.Setting(ctx, settingsKey)
	if errors.Is(err, store.ErrNotFound) {
		return s, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Settings{}, fmt.Errorf("observability: decode settings: %w", err)
	}
	return s, nil
}

// SetSettings validates and stores s, then reconciles the stack to it in
// the background.
func (m *Manager) SetSettings(ctx context.Context, s Settings) (Settings, error) {
	if err := s.Validate(); err != nil {
		return Settings{}, err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return Settings{}, err
	}
	m.settingsMu.Lock()
	err = m.cfg.Store.SetSetting(ctx, settingsKey, string(raw))
	m.settingsMu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return s, nil
}

// AdminPassword returns the Grafana admin password, generating and storing
// it the first time.
func (m *Manager) AdminPassword(ctx context.Context) (string, error) {
	pw, err := m.cfg.Store.Setting(ctx, passwordKey)
	if err == nil {
		return pw, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	if _, err := m.cfg.Store.AddSetting(ctx, passwordKey, randomPassword()); err != nil {
		return "", err
	}
	return m.cfg.Store.Setting(ctx, passwordKey)
}
