package notify

import (
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kyledickey/shed/internal/telemetry"
)

func eventNotifier(st *fakeStore) *Notifier {
	st.channels = []Channel{{ID: "c1", Name: "phone", Provider: "fake", Enabled: true}}
	if st.projects == nil {
		st.projects = map[string]string{"p1": "blog"}
	}
	n := New(st, "https://shed.test/", slog.New(slog.NewTextHandler(io.Discard, nil)))
	n.now = func() time.Time { return t0 }
	return n
}

var web = telemetry.Service{ProjectID: "p1", ID: "s1", Name: "web"}

func TestEventMessages(t *testing.T) {
	tests := []struct {
		name string
		e    telemetry.Event
		want Message // zero Key: nothing sent
	}{
		{"deploy failed", telemetry.Event{Kind: telemetry.EventDeployFailed, ID: "d1", Service: web, Text: "Deployment d1 failed: boom"},
			Message{Key: "deploy:d1:failed", Title: "Deploy failed: web", Body: "Deployment d1 failed: boom",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"backup failed", telemetry.Event{Kind: telemetry.EventBackupFailed, ID: "b1", Service: web, Text: "Backup of web failed: x"},
			Message{Key: "backup:b1:failed", Title: "Backup failed: web", Body: "Backup of web failed: x",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"shed.db backup failed", telemetry.Event{Kind: telemetry.EventBackupFailed, ID: "b2", Text: "Backup of shed.db failed: x"},
			Message{Key: "backup:b2:failed", Title: "Backup failed: shed.db", Body: "Backup of shed.db failed: x"}},
		{"restore failed", telemetry.Event{Kind: telemetry.EventRestoreFailed, ID: "r1", Service: web, Text: "Restore of web failed: x"},
			Message{Key: "restore:r1:failed", Title: "Restore failed: web", Body: "Restore of web failed: x",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"empty text falls back to title", telemetry.Event{Kind: telemetry.EventDeployFailed, ID: "d2", Service: web},
			Message{Key: "deploy:d2:failed", Title: "Deploy failed: web", Body: "Deploy failed: web",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"oom", telemetry.Event{Kind: telemetry.EventCrash, OOM: true, Service: web, Start: t0, Text: "killed"},
			Message{Key: "oom:s1:" + strconv.FormatInt(t0.Unix(), 10), Title: "Out of memory: web", Body: "killed",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"plain crash", telemetry.Event{Kind: telemetry.EventCrash, Service: web, Start: t0, Text: "exited"}, Message{}},
		{"deploy started", telemetry.Event{Kind: telemetry.EventDeployStarted, ID: "d1", Service: web, Text: "x"}, Message{}},
		{"deploy active", telemetry.Event{Kind: telemetry.EventDeployActive, ID: "d1", Service: web, Text: "x"}, Message{}},
		{"backup ok", telemetry.Event{Kind: telemetry.EventBackup, ID: "b1", Service: web, Text: "x"}, Message{}},
		{"restart", telemetry.Event{Kind: telemetry.EventRestart, Service: web, Text: "x"}, Message{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeStore{}
			eventNotifier(st).Event(tt.e)
			if tt.want.Key == "" {
				if len(st.deliveries) != 0 {
					t.Fatalf("sent %+v, want nothing", st.deliveries[0].Message)
				}
				return
			}
			if len(st.deliveries) != 1 || st.deliveries[0].Message != tt.want {
				t.Fatalf("deliveries = %+v, want %+v", st.deliveries, tt.want)
			}
		})
	}
}

func TestEventTruncatesBody(t *testing.T) {
	st := &fakeStore{}
	eventNotifier(st).Event(telemetry.Event{Kind: telemetry.EventDeployFailed, ID: "d", Service: web, Text: strings.Repeat("é", 1000)})
	if got := st.deliveries[0].Body; len(got) > bodyLimit+len("…") || !strings.HasSuffix(got, "…") {
		t.Errorf("body is %d bytes, want at most %d plus an ellipsis", len(got), bodyLimit)
	}
}

func TestEventDisabledRule(t *testing.T) {
	st := &fakeStore{rules: []RuleOverride{{ID: RuleDeployFailed, Enabled: false}}}
	n := eventNotifier(st)
	n.Event(telemetry.Event{Kind: telemetry.EventDeployFailed, ID: "d1", Service: web, Text: "x"})
	if len(st.deliveries) != 0 {
		t.Errorf("disabled rule sent %+v", st.deliveries)
	}
	n.Event(telemetry.Event{Kind: telemetry.EventBackupFailed, ID: "b1", Service: web, Text: "x"})
	if len(st.deliveries) != 1 {
		t.Errorf("other rule sent %d, want 1", len(st.deliveries))
	}
}

func TestEventNoDashboardURL(t *testing.T) {
	st := &fakeStore{}
	n := eventNotifier(st)
	n.baseURL = ""
	n.Event(telemetry.Event{Kind: telemetry.EventDeployFailed, ID: "d1", Service: web, Text: "x"})
	if got := st.deliveries[0].URL; got != "" {
		t.Errorf("URL = %q, want empty", got)
	}
}

func TestCrashLoop(t *testing.T) {
	st := &fakeStore{}
	n := eventNotifier(st)
	clock := t0
	n.now = func() time.Time { return clock }
	crash := func(after time.Duration) {
		clock = clock.Add(after)
		n.Event(telemetry.Event{Kind: telemetry.EventCrash, Service: web, Start: clock, Text: "exited"})
	}

	crash(0)
	crash(time.Minute)
	if len(st.deliveries) != 0 {
		t.Fatal("sent before the threshold")
	}
	crash(time.Minute)
	if len(st.deliveries) != 1 {
		t.Fatalf("sent %d at the threshold, want 1", len(st.deliveries))
	}
	d := st.deliveries[0]
	if d.Key != "crash-loop:s1:"+strconv.FormatInt(clock.Unix(), 10) || d.Title != "Crash loop: web" {
		t.Errorf("message = %+v", d.Message)
	}

	// Muted for 30 minutes, however many crashes come.
	for range 5 {
		crash(time.Minute)
	}
	if len(st.deliveries) != 1 {
		t.Fatalf("sent %d while muted, want 1", len(st.deliveries))
	}

	// After the mute, a fresh run of crashes fires again.
	clock = clock.Add(crashMute)
	crash(0)
	crash(time.Second)
	crash(time.Second)
	if len(st.deliveries) != 2 {
		t.Fatalf("sent %d after the mute, want 2", len(st.deliveries))
	}

	// Old crashes expire and the map does not keep idle services.
	clock = clock.Add(time.Hour)
	other := telemetry.Service{ProjectID: "p1", ID: "s2", Name: "api"}
	n.Event(telemetry.Event{Kind: telemetry.EventCrash, Service: other, Start: clock})
	if _, ok := n.loops["s1"]; ok || len(n.loops) != 1 {
		t.Errorf("loops = %v, want only s2", n.loops)
	}
}

func TestCrashLoopWindowAndThreshold(t *testing.T) {
	st := &fakeStore{rules: []RuleOverride{{ID: RuleCrashLoop, Enabled: true, Threshold: ptr(2.0)}}}
	n := eventNotifier(st)
	clock := t0
	n.now = func() time.Time { return clock }
	crash := func() { n.Event(telemetry.Event{Kind: telemetry.EventCrash, Service: web, Start: clock}) }

	crash()
	clock = clock.Add(crashWindow + time.Second) // the first crash is too old
	crash()
	if len(st.deliveries) != 0 {
		t.Fatal("counted a crash outside the window")
	}
	clock = clock.Add(time.Second)
	crash()
	if len(st.deliveries) != 1 {
		t.Errorf("sent %d with threshold 2, want 1", len(st.deliveries))
	}
}

func TestCrashLoopDisabled(t *testing.T) {
	st := &fakeStore{rules: []RuleOverride{{ID: RuleCrashLoop, Enabled: false}}}
	n := eventNotifier(st)
	for range 5 {
		n.Event(telemetry.Event{Kind: telemetry.EventCrash, Service: web, Start: t0})
	}
	if len(st.deliveries) != 0 || len(n.loops) != 0 {
		t.Errorf("disabled rule sent %d, tracked %d", len(st.deliveries), len(n.loops))
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		threshold *float64
		forSecs   *int
		wantErr   bool
	}{
		{"enabled only", RuleDeployFailed, nil, nil, false},
		{"unknown", "nope", nil, nil, true},
		{"threshold on rule without", RuleDeployFailed, ptr(1.0), nil, true},
		{"duration on rule without", RuleCrashLoop, nil, ptr(5), true},
		{"crash loop 5", RuleCrashLoop, ptr(5.0), nil, false},
		{"crash loop 0", RuleCrashLoop, ptr(0.0), nil, true},
		{"crash loop 2.5", RuleCrashLoop, ptr(2.5), nil, true},
		{"crash loop negative", RuleCrashLoop, ptr(-1.0), nil, true},
	}
	for _, tt := range tests {
		if err := ValidateRule(tt.id, tt.threshold, tt.forSecs); (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, want error %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestMergeRules(t *testing.T) {
	rs := MergeRules([]RuleOverride{
		{ID: RuleCrashLoop, Enabled: false, Threshold: ptr(7.0)},
		{ID: RuleOOM, Enabled: true, Threshold: ptr(9.0)}, // no threshold to override
		{ID: "gone", Enabled: false},
	})
	if len(rs) != len(catalog) {
		t.Fatalf("got %d rules, want %d", len(rs), len(catalog))
	}
	byID := map[string]Rule{}
	for _, r := range rs {
		byID[r.ID] = r
	}
	if r := byID[RuleCrashLoop]; r.Enabled || *r.Threshold != 7 {
		t.Errorf("crash loop = %+v", r)
	}
	if r := byID[RuleOOM]; !r.Enabled || r.Threshold != nil {
		t.Errorf("oom = %+v", r)
	}
	if r := byID[RuleDeployFailed]; !r.Enabled {
		t.Errorf("deploy-failed = %+v, want enabled by default", r)
	}
}
