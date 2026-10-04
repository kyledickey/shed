package backup

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyValidate(t *testing.T) {
	ok := defaultPolicy
	tests := []struct {
		name  string
		edit  func(p *PolicyInput)
		valid bool
	}{
		{"default", func(*PolicyInput) {}, true},
		{"time zone", func(p *PolicyInput) { p.Schedule = "CRON_TZ=Europe/Paris 30 2 * * 1-5" }, true},
		{"descriptor", func(p *PolicyInput) { p.Schedule = "@daily" }, true},
		{"disabled", func(p *PolicyInput) { p.Enabled = false }, true},
		{"keep local 0 with upload", func(p *PolicyInput) { p.KeepLocal = 0 }, true},
		{"no upload, keep remote 0", func(p *PolicyInput) { p.Upload, p.KeepRemote = false, 0 }, true},
		{"empty schedule", func(p *PolicyInput) { p.Schedule = " " }, false},
		{"six fields", func(p *PolicyInput) { p.Schedule = "0 0 3 * * *" }, false},
		{"bad field", func(p *PolicyInput) { p.Schedule = "61 3 * * *" }, false},
		{"bad zone", func(p *PolicyInput) { p.Schedule = "CRON_TZ=Mars/Olympus 0 3 * * *" }, false},
		{"bad compression", func(p *PolicyInput) { p.Compression = "ultra" }, false},
		{"negative keep local", func(p *PolicyInput) { p.KeepLocal = -1 }, false},
		{"negative keep remote", func(p *PolicyInput) { p.KeepRemote = -1 }, false},
		{"keep local 0 without upload", func(p *PolicyInput) { p.KeepLocal, p.Upload = 0, false }, false},
		{"upload, keep remote 0", func(p *PolicyInput) { p.KeepRemote = 0 }, false},
	}
	for _, tt := range tests {
		p := ok
		tt.edit(&p)
		err := p.validate()
		if tt.valid && err != nil {
			t.Errorf("%s: validate() = %v, want nil", tt.name, err)
		}
		if !tt.valid && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: validate() = %v, want ErrInvalid", tt.name, err)
		}
	}
}

func TestParseScheduleZones(t *testing.T) {
	from := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip(err)
	}
	tests := []struct {
		spec string
		want time.Time
	}{
		{"0 3 * * *", time.Date(2026, 10, 5, 3, 0, 0, 0, time.UTC)},
		{"@daily", time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)},
		{"CRON_TZ=Europe/Paris 0 3 * * *", time.Date(2026, 10, 5, 3, 0, 0, 0, paris)},
		{"*/15 * * * *", time.Date(2026, 10, 4, 12, 15, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		s, err := parseSchedule(tt.spec)
		if err != nil {
			t.Fatalf("%s: %v", tt.spec, err)
		}
		if got := s.Next(from); !got.Equal(tt.want) {
			t.Errorf("%s: Next = %v, want %v", tt.spec, got, tt.want)
		}
	}
}
