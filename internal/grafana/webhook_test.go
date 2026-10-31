package grafana

import (
	"strings"
	"testing"
	"time"
)

// webhookSample has the shape Grafana 13 posted in a lab.
const webhookSample = `{
  "receiver": "shed",
  "status": "firing",
  "orgId": 1,
  "groupKey": "{}/{}:{alertname=\"HTTP 5xx ratio high\", shed_service_id=\"s1\"}",
  "externalURL": "https://shed.test/grafana/",
  "version": "1",
  "truncatedAlerts": 0,
  "alerts": [{
    "status": "firing",
    "labels": {"alertname": "HTTP 5xx ratio high", "shed": "default", "shed_service_id": "s1", "shed_service_name": "web"},
    "annotations": {"summary": "more than 5% of requests to web have failed with 5xx"},
    "startsAt": "2026-10-30T12:00:10Z",
    "endsAt": "0001-01-01T00:00:00Z",
    "generatorURL": "https://shed.test/grafana/alerting/grafana/shed-http-errors/view?orgId=1",
    "fingerprint": "7611eef9e67f6e50",
    "silenceURL": "https://shed.test/grafana/alerting/silence/new",
    "values": {"A": 0.5, "C": 1},
    "valueString": "[ var='A' value=0.5 ]"
  }, {
    "status": "resolved",
    "labels": {"alertname": "Host disk almost full"},
    "annotations": {},
    "startsAt": "2026-10-30T11:00:00.5Z",
    "endsAt": "2026-10-30T12:00:00Z",
    "fingerprint": "abc"
  }]
}`

func TestParseWebhook(t *testing.T) {
	alerts, err := ParseWebhook(strings.NewReader(webhookSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 {
		t.Fatalf("got %d alerts, want 2", len(alerts))
	}
	a := alerts[0]
	if a.Status != AlertFiring || a.Fingerprint != "7611eef9e67f6e50" ||
		!a.StartsAt.Equal(time.Date(2026, 10, 30, 12, 0, 10, 0, time.UTC)) ||
		a.Labels["shed_service_id"] != "s1" || a.Labels["alertname"] != "HTTP 5xx ratio high" ||
		a.Annotations["summary"] != "more than 5% of requests to web have failed with 5xx" ||
		a.GeneratorURL != "https://shed.test/grafana/alerting/grafana/shed-http-errors/view?orgId=1" {
		t.Errorf("first alert = %+v", a)
	}
	if alerts[1].Status != AlertResolved {
		t.Errorf("second alert = %+v", alerts[1])
	}
}

func TestParseWebhookRejects(t *testing.T) {
	const ok = `"status":"firing","fingerprint":"f","startsAt":"2026-10-30T12:00:00Z"`
	tests := []struct {
		name, body string
	}{
		{"empty", ``},
		{"not JSON", `hello`},
		{"truncated", `{"alerts":[{`},
		{"two documents", `{"alerts":[]} {"alerts":[]}`},
		{"trailing garbage", `{"alerts":[]} x`},
		{"no alerts field", `{"status":"firing"}`},
		{"null alerts", `{"alerts":null}`},
		{"alerts of the wrong type", `{"alerts":{}}`},
		{"unknown status", `{"alerts":[{"status":"pending","fingerprint":"f","startsAt":"2026-10-30T12:00:00Z"}]}`},
		{"no fingerprint", `{"alerts":[{"status":"firing","startsAt":"2026-10-30T12:00:00Z"}]}`},
		{"no start", `{"alerts":[{"status":"firing","fingerprint":"f"}]}`},
		{"bad start", `{"alerts":[{"status":"firing","fingerprint":"f","startsAt":"today"}]}`},
		{"zero start", `{"alerts":[{"status":"firing","fingerprint":"f","startsAt":"0001-01-01T00:00:00Z"}]}`},
		{"label of the wrong type", `{"alerts":[{` + ok + `,"labels":{"a":1}}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if a, err := ParseWebhook(strings.NewReader(tt.body)); err == nil {
				t.Errorf("ParseWebhook(%s) = %+v, want an error", tt.body, a)
			}
		})
	}
	if a, err := ParseWebhook(strings.NewReader(`{"alerts":[]}`)); err != nil || len(a) != 0 {
		t.Errorf("no alerts = %+v, %v", a, err)
	}
}

func TestWebhookAlertSummary(t *testing.T) {
	summary := map[string]string{"summary": "the host disk is more than 85% full"}
	for _, tt := range []struct {
		alertname, want string
	}{
		{"Host disk almost full", "the host disk is more than 85% full"},
		{"DatasourceError", "Grafana could not evaluate the rule Host disk almost full."},
		{"DatasourceNoData", "Grafana could not evaluate the rule Host disk almost full."},
	} {
		a := WebhookAlert{Annotations: summary, Labels: map[string]string{"alertname": tt.alertname, "rulename": "Host disk almost full"}}
		if got := a.Summary(); got != tt.want {
			t.Errorf("%s: Summary() = %q, want %q", tt.alertname, got, tt.want)
		}
	}
}
