// Package hark sends push notifications through a Hark service webhook
// (https://hark.ryan.ceo). A webhook URL embeds a secret token, so the
// package never puts the URL or token into an error.
package hark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits from the Hark API. Body is also capped at 16 KiB of UTF-8.
const (
	maxBody      = 8000
	maxBodyBytes = 16 << 10
	maxSummary   = 500
	maxTitle     = 80
	maxProject   = 80
	maxKey       = 200
)

// ErrUnknownToken is returned when Hark does not recognize the webhook
// token, usually because it was rotated.
var ErrUnknownToken = errors.New("hark: unknown or rotated webhook token")

// RateLimitError is returned when Hark rejects a request for exceeding a
// per-minute limit or the monthly allowance.
type RateLimitError struct {
	// RetryAfter is how long Hark asked the sender to wait. It is zero when
	// Hark gave no hint, which means the monthly allowance is used up.
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("hark: rate limited, retry after %s", e.RetryAfter)
	}
	return "hark: rate limited, monthly allowance exhausted"
}

// Notification is one push notification.
type Notification struct {
	// Body is the message text. It is required.
	Body string
	// Title overrides the sender name shown on the notification.
	Title string
	// Summary replaces Body in push banners and list previews.
	Summary string
	// URL is opened when the notification is tapped.
	URL string
	// Project groups notifications in the app inbox.
	Project string
}

// Result describes an accepted notification.
type Result struct {
	// EventID identifies the event in the Hark dashboard. It may be empty
	// when Hark is still processing an identical earlier request.
	EventID string
	// Delivered is the number of devices the push was sent to. Zero means
	// no device is registered.
	Delivered int
}

// Client sends notifications to one Hark service. It is safe for
// concurrent use.
type Client struct {
	url   string
	token string
	hc    *http.Client
}

// New returns a Client for webhookURL, which has the form
// https://hark.ryan.ceo/hooks/<token>. Plain http is accepted only for
// loopback hosts. A nil hc uses a client with a 10 second timeout. New does
// not contact Hark.
func New(webhookURL string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return nil, errors.New("hark: webhook URL is not a valid URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return nil, errors.New("hark: webhook URL must use https")
	}
	if u.Hostname() == "" {
		return nil, errors.New("hark: webhook URL has no host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("hark: webhook URL must not have credentials, a query, or a fragment")
	}
	token, ok := strings.CutPrefix(u.Path, "/hooks/")
	if !ok || token == "" || strings.Contains(token, "/") {
		return nil, errors.New("hark: webhook URL path must be /hooks/<token>")
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{url: u.String(), token: token, hc: hc}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type request struct {
	Body    string `json:"body"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
	URL     string `json:"url,omitempty"`
	Project string `json:"project,omitempty"`
}

type response struct {
	EventID           string `json:"eventId"`
	Delivered         int    `json:"delivered"`
	Error             string `json:"error"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
}

// Send delivers n. Text fields longer than Hark allows are truncated
// rather than rejected. A non-empty idempotencyKey makes repeated sends of
// the same notification deliver it once; it must be at most 200 characters.
//
// Send returns ErrUnknownToken for an unknown token and a *RateLimitError
// when rate limited.
func (c *Client) Send(ctx context.Context, n Notification, idempotencyKey string) (Result, error) {
	body := strings.TrimSpace(n.Body)
	if body == "" {
		return Result{}, errors.New("hark: notification body is empty")
	}
	if len(idempotencyKey) > maxKey {
		return Result{}, fmt.Errorf("hark: idempotency key is longer than %d characters", maxKey)
	}
	payload, err := json.Marshal(request{
		Body:    truncate(body, maxBody, maxBodyBytes),
		Title:   truncate(n.Title, maxTitle, len(n.Title)),
		Summary: truncate(n.Summary, maxSummary, len(n.Summary)),
		URL:     n.URL,
		Project: truncate(n.Project, maxProject, len(n.Project)),
	})
	if err != nil {
		return Result{}, fmt.Errorf("hark: encode notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return Result{}, errors.New("hark: build request")
	}
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		// A *url.Error message contains the URL, and with it the token.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return Result{}, fmt.Errorf("hark: send: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return Result{}, fmt.Errorf("hark: read response: %w", err)
	}
	var r response
	decodeErr := json.Unmarshal(raw, &r)

	switch resp.StatusCode {
	case http.StatusOK:
		if decodeErr != nil {
			return Result{}, fmt.Errorf("hark: decode response: %w", decodeErr)
		}
		return Result{EventID: r.EventID, Delivered: r.Delivered}, nil
	case http.StatusAccepted:
		return Result{EventID: r.EventID, Delivered: r.Delivered}, nil
	case http.StatusNotFound:
		return Result{}, ErrUnknownToken
	case http.StatusTooManyRequests:
		retry := time.Duration(r.RetryAfterSeconds) * time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
			retry = time.Duration(s) * time.Second
		}
		return Result{}, &RateLimitError{RetryAfter: retry}
	}

	msg := r.Error
	if msg == "" {
		msg = r.Message
	}
	if msg == "" && decodeErr != nil {
		msg = truncate(strings.TrimSpace(string(raw)), 200, 200)
	}
	msg = strings.ReplaceAll(msg, c.token, "<token>")
	if msg == "" {
		return Result{}, fmt.Errorf("hark: %s", resp.Status)
	}
	return Result{}, fmt.Errorf("hark: %s: %s", resp.Status, msg)
}

// truncate shortens s to at most maxRunes runes and maxBytes bytes, ending
// it with an ellipsis when anything was cut.
func truncate(s string, maxRunes, maxBytes int) string {
	if utf8.RuneCountInString(s) <= maxRunes && len(s) <= maxBytes {
		return s
	}
	const ellipsis = "…"
	maxRunes--
	maxBytes -= len(ellipsis)
	end, n := 0, 0
	for end < len(s) {
		_, w := utf8.DecodeRuneInString(s[end:])
		if n == maxRunes || end+w > maxBytes {
			break
		}
		end += w
		n++
	}
	return s[:end] + ellipsis
}
