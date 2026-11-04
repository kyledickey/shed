package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// ntfyMaxMessage is ntfy.sh's default message size limit; longer messages
// become attachments, which a self-hosted server may not allow.
const ntfyMaxMessage = 4096

var ntfyTopic = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// Ntfy returns the provider for ntfy push notifications. Its target is a
// topic URL, https://<host>[/<base path>]/<topic>, with an optional access
// token (https://:<token>@host/topic) or username and password as userinfo.
// hc is the HTTP client used to call it; nil uses a client with a 10 second
// timeout.
func Ntfy(hc *http.Client) Provider {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return Provider{
		Name: "ntfy",
		Open: func(target string) (Sender, error) { return openNtfy(target, hc) },
	}
}

type ntfySender struct {
	url        string // the server's base URL, ending in "/"
	topic      string
	user, pass string
	hc         *http.Client
}

// openNtfy parses a topic URL. Errors never echo the URL, since it may hold
// a token or password.
func openNtfy(topicURL string, hc *http.Client) (ntfySender, error) {
	u, err := url.Parse(topicURL)
	if err != nil {
		return ntfySender{}, errors.New("ntfy: topic URL is not a valid URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return ntfySender{}, errors.New("ntfy: topic URL must use https")
	}
	if u.Hostname() == "" {
		return ntfySender{}, errors.New("ntfy: topic URL has no host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return ntfySender{}, errors.New("ntfy: topic URL must not have a query or a fragment")
	}
	dir, topic := path.Split(u.Path)
	if !ntfyTopic.MatchString(topic) {
		return ntfySender{}, errors.New(
			"ntfy: topic URL must end in a topic of up to 64 letters, digits, - or _",
		)
	}
	s := ntfySender{topic: topic, hc: hc}
	if u.User != nil {
		s.user = u.User.Username()
		s.pass, _ = u.User.Password()
	}
	u.User, u.Path, u.RawPath = nil, dir, ""
	s.url = u.String()
	return s, nil
}

type ntfyRequest struct {
	Topic   string   `json:"topic"`
	Message string   `json:"message"`
	Title   string   `json:"title,omitempty"`
	Click   string   `json:"click,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// Send publishes m to the topic. ntfy has no idempotency key, so m.Key is
// ignored.
func (s ntfySender) Send(ctx context.Context, m Message) error {
	r := ntfyRequest{
		Topic:   s.topic,
		Message: truncate(strings.TrimSpace(m.Body), ntfyMaxMessage, ntfyMaxMessage),
		Title:   m.Title,
		Click:   m.URL,
	}
	if m.Project != "" {
		r.Tags = []string{m.Project}
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("ntfy: encode notification: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(payload))
	if err != nil {
		return errors.New("ntfy: build request")
	}
	req.Header.Set("Content-Type", "application/json")
	if s.user != "" || s.pass != "" {
		req.SetBasicAuth(s.user, s.pass)
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("ntfy: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	err = fmt.Errorf("ntfy: %s", resp.Status)
	if msg := s.redact(body.Error); msg != "" {
		err = fmt.Errorf("ntfy: %s: %s", resp.Status, msg)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return permanent(err)
	}
	return err
}

func (s ntfySender) redact(msg string) string {
	if s.pass != "" {
		msg = strings.ReplaceAll(msg, s.pass, "<password>")
	}
	return msg
}
