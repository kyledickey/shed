package notify

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAlert(t *testing.T) {
	start := time.Date(2026, 10, 30, 12, 0, 10, 0, time.UTC)
	const key = "alert:fp:2026-10-30T12:00:10Z:"
	const grafanaURL = "https://shed.test/grafana/alerting/grafana/x/view"
	tests := []struct {
		name string
		a    AlertInfo
		want Message
	}{
		{"known service",
			AlertInfo{Name: "HTTP 5xx ratio high", Status: "firing", Fingerprint: "fp", StartsAt: start,
				Summary: "web fails", ServiceID: "s1", GeneratorURL: grafanaURL},
			Message{Key: key + "firing", Title: "HTTP 5xx ratio high", Body: "web fails",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"resolved",
			AlertInfo{Name: "HTTP 5xx ratio high", Status: "resolved", Fingerprint: "fp", StartsAt: start,
				Summary: "web fails", ServiceID: "s1", GeneratorURL: grafanaURL},
			Message{Key: key + "resolved", Title: "Resolved: HTTP 5xx ratio high", Body: "web fails",
				URL: "https://shed.test/projects/p1/services/s1", Project: "blog"}},
		{"no service",
			AlertInfo{Name: "Host disk almost full", Status: "firing", Fingerprint: "fp", StartsAt: start,
				Summary: "disk", GeneratorURL: grafanaURL},
			Message{Key: key + "firing", Title: "Host disk almost full", Body: "disk", URL: grafanaURL}},
		{"unknown service of a custom rule, no summary",
			AlertInfo{Name: "My rule", Status: "firing", Fingerprint: "fp", StartsAt: start,
				ServiceID: "gone", GeneratorURL: grafanaURL},
			Message{Key: key + "firing", Title: "My rule", Body: "My rule", URL: grafanaURL}},
		{"custom rule without a name",
			AlertInfo{Status: "firing", Fingerprint: "fp", StartsAt: start},
			Message{Key: key + "firing", Title: "Grafana alert", Body: "Grafana alert"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeStore{services: map[string]string{"s1": "p1"}}
			n := eventNotifier(st)
			if err := n.Alert(context.Background(), tt.a); err != nil {
				t.Fatal(err)
			}
			if len(st.deliveries) != 1 || st.deliveries[0].Message != tt.want {
				t.Fatalf("deliveries = %+v, want %+v", st.deliveries, tt.want)
			}
		})
	}
}

func TestAlertOnePerFiring(t *testing.T) {
	st := &fakeStore{}
	n := eventNotifier(st)
	ctx := context.Background()
	start := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	send := func(status string, at time.Time) {
		t.Helper()
		a := AlertInfo{Name: "x", Status: status, Fingerprint: "fp", StartsAt: at, Summary: "s"}
		if err := n.Alert(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	send("firing", start)
	send("firing", start) // Grafana's repeat
	send("resolved", start)
	send("resolved", start)
	send("firing", start.Add(time.Hour)) // a new firing
	var got []string
	for _, d := range st.deliveries {
		got = append(got, d.Title)
	}
	if want := "x,Resolved: x,x"; strings.Join(got, ",") != want {
		t.Errorf("sent %v, want %s", got, want)
	}
}

func TestAlertLookupFailure(t *testing.T) {
	boom := errors.New("boom")
	st := &fakeStore{servicesErr: boom}
	n := eventNotifier(st)
	a := AlertInfo{Name: "x", Status: "firing", Fingerprint: "fp", StartsAt: time.Now(), ServiceID: "s1"}
	if err := n.Alert(context.Background(), a); !errors.Is(err, boom) {
		t.Errorf("Alert = %v, want the store's error so that Grafana retries", err)
	}
	if len(st.deliveries) != 0 {
		t.Errorf("sent %+v", st.deliveries)
	}
}

func TestAlertTruncatesBody(t *testing.T) {
	st := &fakeStore{}
	a := AlertInfo{Name: "x", Status: "firing", Fingerprint: "fp", StartsAt: time.Now(), Summary: strings.Repeat("é", 1000)}
	if err := eventNotifier(st).Alert(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if got := st.deliveries[0].Body; len(got) > bodyLimit+len("…") || !strings.HasSuffix(got, "…") {
		t.Errorf("body is %d bytes", len(got))
	}
}
