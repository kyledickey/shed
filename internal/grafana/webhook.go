package grafana

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// The statuses of a [WebhookAlert].
const (
	AlertFiring   = "firing"
	AlertResolved = "resolved"
)

// WebhookAlert is one alert of a webhook call from Grafana.
type WebhookAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	Fingerprint  string            `json:"fingerprint"`
	GeneratorURL string            `json:"generatorURL"`
}

// Summary returns the text of a. When a rule can't be evaluated Grafana sends
// DatasourceError or DatasourceNoData with the rule's annotations, whose summary
// would claim the condition holds, so those name the rule instead.
func (a WebhookAlert) Summary() string {
	switch a.Labels["alertname"] {
	case "DatasourceError", "DatasourceNoData":
		return "Grafana could not evaluate the rule " + a.Labels["rulename"] + "."
	}
	return a.Annotations["summary"]
}

// ParseWebhook decodes the body of a webhook call from Grafana and returns its
// alerts. It accepts one JSON document whose alerts each have a status,
// fingerprint, and start time, and ignores other fields, since Grafana adds
// them between versions.
func ParseWebhook(r io.Reader) ([]WebhookAlert, error) {
	var body struct {
		Alerts []WebhookAlert `json:"alerts"`
	}
	dec := json.NewDecoder(r)
	if err := dec.Decode(&body); err != nil {
		return nil, fmt.Errorf("grafana: decode webhook: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("grafana: decode webhook: data after the JSON document")
	}
	if body.Alerts == nil {
		return nil, errors.New("grafana: decode webhook: no alerts field")
	}
	for i, a := range body.Alerts {
		switch {
		case a.Status != AlertFiring && a.Status != AlertResolved:
			return nil, fmt.Errorf("grafana: webhook alert %d: unknown status %q", i, a.Status)
		case a.Fingerprint == "":
			return nil, fmt.Errorf("grafana: webhook alert %d: no fingerprint", i)
		case a.StartsAt.IsZero():
			return nil, fmt.Errorf("grafana: webhook alert %d: no start time", i)
		}
	}
	return body.Alerts, nil
}
