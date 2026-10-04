package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/kyledickey/shed/internal/store"
)

// PolicyInput is the user-editable part of a backup policy.
type PolicyInput struct {
	Enabled bool
	// Schedule is a 5-field cron expression or a descriptor such as @daily,
	// in UTC unless it starts with CRON_TZ=<zone>.
	Schedule string
	// Compression is fastest, default, better, or best.
	Compression string
	// KeepLocal is how many scheduled backups keep their archive on disk.
	// It may be 0 only when uploading.
	KeepLocal int
	// Upload stores archives in S3 too, while S3 is configured.
	Upload bool
	// KeepRemote is how many scheduled backups keep their object in S3.
	KeepRemote int
}

// Policy is a backup policy and the time of its next scheduled run.
type Policy struct {
	PolicyInput
	// NextRun is nil while the policy is disabled.
	NextRun *time.Time
}

// defaultPolicy applies to services without a stored policy, and to shed.db
// until its policy is changed.
var defaultPolicy = PolicyInput{
	Enabled:     true,
	Schedule:    "0 3 * * *",
	Compression: compressBest,
	KeepLocal:   7,
	Upload:      true,
	KeepRemote:  30,
}

// parseSchedule parses a cron schedule. Schedules without a time zone are
// in UTC.
func parseSchedule(spec string) (cron.Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, errors.New("schedule is empty")
	}
	if !strings.HasPrefix(spec, "CRON_TZ=") && !strings.HasPrefix(spec, "TZ=") {
		spec = "CRON_TZ=UTC " + spec
	}
	s, err := cron.ParseStandard(spec)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// validate checks p and returns an error wrapping ErrInvalid if it is
// unusable.
func (p PolicyInput) validate() error {
	if _, err := parseSchedule(p.Schedule); err != nil {
		return invalidf("invalid schedule %q: %v", p.Schedule, err)
	}
	if _, err := encoderOptions(p.Compression); err != nil {
		return invalidf("compression must be fastest, default, better, or best")
	}
	if p.KeepLocal < 0 || p.KeepRemote < 0 {
		return invalidf("the number of backups to keep cannot be negative")
	}
	if p.KeepLocal == 0 && !p.Upload {
		return invalidf("keep at least one backup locally, or upload them")
	}
	if p.Upload && p.KeepRemote == 0 {
		return invalidf("keep at least one backup in S3 when uploading")
	}
	return nil
}

func policyFromStore(p store.BackupPolicy) PolicyInput {
	return PolicyInput{
		Enabled:     p.Enabled,
		Schedule:    p.Schedule,
		Compression: p.Compression,
		KeepLocal:   p.KeepLocal,
		Upload:      p.Upload,
		KeepRemote:  p.KeepRemote,
	}
}

// systemPolicy is the JSON form of the shed.db policy in settings.
type systemPolicy struct {
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
	Compression string `json:"compression"`
	KeepLocal   int    `json:"keepLocal"`
	Upload      bool   `json:"upload"`
	KeepRemote  int    `json:"keepRemote"`
}

// policy returns the stored policy of a target ("" = shed.db), or the
// default. It does not check that a service exists.
func (m *Manager) policy(ctx context.Context, serviceID string) (PolicyInput, error) {
	if serviceID == "" {
		v, err := m.store.Setting(ctx, keySystemPolicy)
		if errors.Is(err, store.ErrNotFound) {
			return defaultPolicy, nil
		}
		if err != nil {
			return PolicyInput{}, fmt.Errorf("backup: %w", err)
		}
		var sp systemPolicy
		if err := json.Unmarshal([]byte(v), &sp); err != nil {
			return PolicyInput{}, fmt.Errorf("backup: decode %s: %w", keySystemPolicy, err)
		}
		return PolicyInput(sp), nil
	}
	p, err := m.store.BackupPolicy(ctx, serviceID)
	if errors.Is(err, store.ErrNotFound) {
		return defaultPolicy, nil
	}
	if err != nil {
		return PolicyInput{}, fmt.Errorf("backup: %w", err)
	}
	return policyFromStore(p), nil
}

// Policy returns the backup policy of a service, or of shed.db for an empty
// serviceID. Services without a stored policy get the default one. It
// returns store.ErrNotFound for an unknown service.
func (m *Manager) Policy(ctx context.Context, serviceID string) (Policy, error) {
	if err := m.checkService(ctx, serviceID); err != nil {
		return Policy{}, err
	}
	p, err := m.policy(ctx, serviceID)
	if err != nil {
		return Policy{}, err
	}
	return Policy{PolicyInput: p, NextRun: m.nextRun(serviceID, p, m.now())}, nil
}

// SetPolicy validates and stores the backup policy of a service, or of
// shed.db for an empty serviceID, and reschedules its next run. Invalid
// policies are rejected with an error wrapping ErrInvalid.
func (m *Manager) SetPolicy(ctx context.Context, serviceID string, in PolicyInput) (Policy, error) {
	if err := in.validate(); err != nil {
		return Policy{}, err
	}
	if err := m.checkService(ctx, serviceID); err != nil {
		return Policy{}, err
	}
	in.Schedule = strings.TrimSpace(in.Schedule)
	if serviceID == "" {
		v, err := json.Marshal(systemPolicy(in))
		if err != nil {
			return Policy{}, fmt.Errorf("backup: encode system policy: %w", err)
		}
		if err := m.store.SetSetting(ctx, keySystemPolicy, string(v)); err != nil {
			return Policy{}, fmt.Errorf("backup: %w", err)
		}
	} else {
		err := m.store.PutBackupPolicy(ctx, store.BackupPolicy{
			ServiceID:   serviceID,
			Enabled:     in.Enabled,
			Schedule:    in.Schedule,
			Compression: in.Compression,
			KeepLocal:   in.KeepLocal,
			Upload:      in.Upload,
			KeepRemote:  in.KeepRemote,
		})
		if err != nil {
			return Policy{}, fmt.Errorf("backup: %w", err)
		}
	}
	m.mu.Lock()
	delete(m.next, serviceID)
	m.mu.Unlock()
	return Policy{PolicyInput: in, NextRun: m.nextRun(serviceID, in, m.now())}, nil
}

// scheduled is the next run of a target under a given schedule.
type scheduled struct {
	spec string
	at   time.Time
}

// nextRun returns when the target runs next under p, or nil if p is
// disabled. The time is computed once per schedule and then kept, so that
// runs missed while shed was down are skipped rather than caught up.
func (m *Manager) nextRun(target string, p PolicyInput, now time.Time) *time.Time {
	if !p.Enabled {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.next[target]; ok && s.spec == p.Schedule {
		at := s.at
		return &at
	}
	sched, err := parseSchedule(p.Schedule)
	if err != nil {
		return nil
	}
	at := sched.Next(now)
	if at.IsZero() {
		return nil
	}
	m.next[target] = scheduled{spec: p.Schedule, at: at}
	return &at
}

// tick starts the scheduled backups that are due at now and advances their
// next run.
func (m *Manager) tick(ctx context.Context, now time.Time) {
	svs, err := m.store.ServicesWithVolumes(ctx)
	if err != nil {
		m.log.Error("backup: list services to back up", "err", err)
		return
	}
	targets := []string{""}
	for _, sv := range svs {
		targets = append(targets, sv.ID)
	}
	live := make(map[string]bool, len(targets))
	for _, id := range targets {
		live[id] = true
		p, err := m.policy(ctx, id)
		if err != nil {
			m.log.Error("backup: load policy", "service", id, "err", err)
			continue
		}
		next := m.nextRun(id, p, now)
		if next == nil {
			m.mu.Lock()
			delete(m.next, id)
			m.mu.Unlock()
			continue
		}
		if now.Before(*next) {
			continue
		}
		sched, _ := parseSchedule(p.Schedule) // Valid: nextRun parsed it.
		m.mu.Lock()
		m.next[id] = scheduled{spec: p.Schedule, at: sched.Next(now)}
		m.mu.Unlock()

		if _, err := m.enqueueBackup(ctx, id, store.BackupSchedule); err != nil {
			switch {
			case errors.Is(err, ErrBusy):
				m.log.Info("backup: skip scheduled backup, one is already queued", "service", id)
			case errors.Is(err, errNotDeployed):
				m.log.Debug("backup: skip scheduled backup of a service that was never deployed", "service", id)
			default:
				m.log.Error("backup: queue scheduled backup", "service", id, "err", err)
			}
		}
	}
	m.mu.Lock()
	for id := range m.next {
		if !live[id] {
			delete(m.next, id)
		}
	}
	m.mu.Unlock()
}
