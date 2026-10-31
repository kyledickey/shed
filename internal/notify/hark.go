package notify

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

// Field limits from the Hark API (https://hark.ryan.ceo/docs). Body is also
// capped at 16 KiB of UTF-8.
const (
	harkMaxBody      = 8000
	harkMaxBodyBytes = 16 << 10
	harkMaxTitle     = 80
	harkMaxProject   = 80
	harkMaxKey       = 200
)

var errHarkUnknownToken = errors.New("hark: unknown or rotated webhook token")

// Hark returns the provider for Hark push notifications. Its target is a
// Hark service webhook URL, https://hark.ryan.ceo/hooks/<token>, whose token
// is the only credential. hc is the HTTP client used to call it; nil uses a
// client with a 10 second timeout.
func Hark(hc *http.Client) Provider {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return Provider{
		Name: "hark",
		Open: func(target string) (Sender, error) { return openHark(target, hc) },
	}
}

type harkSender struct {
	url   string
	token string
	hc    *http.Client
}

// openHark parses a webhook URL. Plain http is accepted only for loopback
// hosts, which tests use. Errors never echo the URL, since it holds the token.
func openHark(webhookURL string, hc *http.Client) (harkSender, error) {
	u, err := url.Parse(webhookURL)
	if err != nil {
		return harkSender{}, errors.New("hark: webhook URL is not a valid URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return harkSender{}, errors.New("hark: webhook URL must use https")
	}
	if u.Hostname() == "" {
		return harkSender{}, errors.New("hark: webhook URL has no host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return harkSender{}, errors.New(
			"hark: webhook URL must not have credentials, a query, or a fragment",
		)
	}
	token, ok := strings.CutPrefix(u.Path, "/hooks/")
	if !ok || token == "" || strings.Contains(token, "/") {
		return harkSender{}, errors.New("hark: webhook URL path must be /hooks/<token>")
	}
	return harkSender{url: u.String(), token: token, hc: hc}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type harkRequest struct {
	Body    string `json:"body"`
	Title   string `json:"title,omitempty"`
	URL     string `json:"url,omitempty"`
	Project string `json:"project,omitempty"`
}

type harkResponse struct {
	EventID           string `json:"eventId"`
	Delivered         int    `json:"delivered"`
	Error             string `json:"error"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
}

// Send posts m to the webhook, truncating text that is longer than Hark
// allows rather than failing. m.Key becomes the Idempotency-Key, so a
// repeated send of the same message is delivered once.
func (s harkSender) Send(ctx context.Context, m Message) error {
	if len(m.Key) > harkMaxKey {
		return fmt.Errorf("hark: idempotency key is longer than %d characters", harkMaxKey)
	}
	body := strings.TrimSpace(m.Body)
	payload, err := json.Marshal(harkRequest{
		Body:    truncate(body, harkMaxBody, harkMaxBodyBytes),
		Title:   truncate(m.Title, harkMaxTitle, len(m.Title)),
		URL:     m.URL,
		Project: truncate(m.Project, harkMaxProject, len(m.Project)),
	})
	if err != nil {
		return fmt.Errorf("hark: encode notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return errors.New("hark: build request")
	}
	req.Header.Set("Content-Type", "application/json")
	if m.Key != "" {
		req.Header.Set("Idempotency-Key", m.Key)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		// A *url.Error message contains the URL, and with it the token.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("hark: send: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("hark: read response: %w", err)
	}
	var r harkResponse
	decodeErr := json.Unmarshal(raw, &r)

	switch resp.StatusCode {
	case http.StatusOK:
		if decodeErr != nil {
			return fmt.Errorf("hark: decode response: %w", decodeErr)
		}
		if r.Delivered == 0 {
			return permanent(ErrNotDelivered)
		}
		return nil
	case http.StatusAccepted:
		// An identical request is still being processed.
		return nil
	case http.StatusNotFound:
		return permanent(errHarkUnknownToken)
	case http.StatusTooManyRequests:
		if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 {
			r.RetryAfterSeconds = secs
		}
		if r.RetryAfterSeconds > 0 {
			return fmt.Errorf("hark: rate limited, retry after %ds", r.RetryAfterSeconds)
		}
		return errors.New("hark: rate limited, monthly allowance exhausted")
	}

	msg := r.Error
	if msg == "" {
		msg = r.Message
	}
	if msg == "" && decodeErr != nil {
		msg = truncate(strings.TrimSpace(string(raw)), 200, 200)
	}
	msg = strings.ReplaceAll(msg, s.token, "<token>")
	err = fmt.Errorf("hark: %s", resp.Status)
	if msg != "" {
		err = fmt.Errorf("hark: %s: %s", resp.Status, msg)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return permanent(err)
	}
	return err
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
