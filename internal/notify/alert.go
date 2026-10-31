package notify

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// AlertInfo is one alert that Grafana sent to shed.
type AlertInfo struct {
	Name string
	// Status is "firing" or "resolved".
	Status      string
	Fingerprint string
	StartsAt    time.Time
	Summary     string
	// ServiceID is the service the alert is about, or "". It may name no
	// service, since custom Grafana rules set it however they like.
	ServiceID string
	// GeneratorURL is the rule's page in Grafana.
	GeneratorURL string
}

// Alert enqueues a message about a. Every alert passes: a paused rule doesn't
// fire, and custom Grafana rules aren't in the catalog. The key holds the
// firing's identity and status, so Grafana's repeat notifications and retries
// are dropped, and each firing sends once and its end sends once.
//
// shortcut: the outbox is the only memory of what was sent, so a firing that
// outlasts its 30-day retention or gets a new startsAt from a rule edit sends
// again at the next 4-hour repeat; track firings if that proves noisy.
func (n *Notifier) Alert(ctx context.Context, a AlertInfo) error {
	name := a.Name
	if strings.TrimSpace(name) == "" {
		name = "Grafana alert"
	}
	title := name
	if a.Status == "resolved" {
		title = "Resolved: " + name
	}
	m := Message{
		Key:   fmt.Sprintf("alert:%s:%s:%s", a.Fingerprint, a.StartsAt.UTC().Format(time.RFC3339Nano), a.Status),
		Title: title,
		Body:  messageBody(a.Summary, name),
		URL:   a.GeneratorURL,
	}
	if a.ServiceID != "" {
		projectID, err := n.store.ServiceProject(ctx, a.ServiceID)
		if err != nil {
			return fmt.Errorf("notify: service of alert %s: %w", a.Fingerprint, err)
		}
		if projectID != "" {
			if m.Project, err = n.store.ProjectName(ctx, projectID); err != nil {
				return fmt.Errorf("notify: project of alert %s: %w", a.Fingerprint, err)
			}
			if n.baseURL != "" {
				m.URL = fmt.Sprintf("%s/projects/%s/services/%s", n.baseURL, projectID, a.ServiceID)
			}
		}
	}
	return n.Enqueue(ctx, m)
}
